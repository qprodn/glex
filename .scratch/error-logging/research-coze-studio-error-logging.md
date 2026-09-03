# 调研笔记：coze-dev/coze-studio（Go 后端）的错误日志策略

- 调研日期：2026-09-02
- 源码：github.com/coze-dev/coze-studio `main` 分支浅克隆（sparse checkout，仅 backend/）
- HEAD commit：`fefb05ff27be1da939612fbf9faf5db62583b8ae`（2026-07-29，"fix(infra): align statefulset servicename with actual service name (#2723)"）
- 以下所有行号以该 commit 为准。纯传播 = `return err` / `return nil, err`，不做任何包装与日志。

## TL;DR

coze-studio **没有"错误只记一次"的成文约定**，实际策略是三层并存：

1. **domain/application 层大量"log-and-return"**——遇到 `err != nil` 先 `logs.CtxErrorf` 再 `return`，错误继续向上传播（`backend/domain/knowledge/processor/impl/custom_table.go:38-46`、`backend/application/knowledge/knowledge.go:65-67`）。这类位点在 domain+application 有约 281 处（紧邻 `return` 的 Error/Warn 日志），Error 级调用点共 530 处（domain 299 / application 159 / infra 35 / api 21）。
2. **HTTP 边界兜底**——生成的 handler 统一调 `httputil.InternalError`：业务错误（`errorx.StatusError`）打 **Warn** 并返回 HTTP 200 + `{code,msg}`；其余（含裸 gorm 错误）打 **Error** 并返回 500。外层 `AccessLogMW` 再按响应状态码补一行访问日志（≥500 Error、≥400 Warn、其余 Info）。同一错误因此常被打 2~4 次。
3. **预期内条件静默**——DAL 层把 `gorm.ErrRecordNotFound` 静默映射为 `nil,nil`/`exist=false`；redis.Nil 被缓存库设为"未命中"哨兵；workflow 域把 `context.Canceled` 映射为取消事件，均不打日志。

级别上 Error 是默认值且被明显滥用（业务"not found"、会话失效都打 Error）；没有结构化字段，全部 printf 风格，错误经 `%v` 拼进消息文本。

---

## a) 日志库与封装

**自研薄封装 `backend/pkg/logs`，底层不是 zap/logrus**，而是标准库 `log` 写 stderr 的纯文本 logger，API 克隆了 CloudWeGo hlog 的接口形状。

- `backend/pkg/logs/logger.go:26-71`：定义 `FormatLogger`/`Logger`/`CtxLogger`/`FullLogger` 接口，级别为 `Trace/Debug/Info/Notice/Warn/Error/Fatal`（:79-87）。
- `backend/pkg/logs/default.go:29-32`：全局默认 logger 是 `defaultLogger{stdlog: log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile|log.Lmicroseconds)}`，**纯文本、stderr、无 JSON**。
- `backend/pkg/logs/default.go:192-210`：`logfCtx` 唯一的"结构化"处理是把 ctx 里的 logID 文本追加为 `[log-id: %v]`。没有从 ctx 提取 user_id/trace_id 等字段，没有 `slog.Attr` 类结构。
- zap（`go.mod:262`）和 logrus（`go.mod:221`）都只是 `// indirect` 依赖，backend 源码中无直接使用（`grep -rln "zap\.\|logrus\."` 为空）；也没有 `hlog.SetLogger` 调用把封装桥接进 hertz——hertz 内部（如 panic recover）走它自己的默认 hlog，与 `pkg/logs` 是两条通道。
- 封装 API 是 `logs.CtxErrorf(ctx, format, args...)` / `logs.CtxWarnf` / `logs.Errorf` 风格（`default.go:63-161`）。注意**部分代码用不带 ctx 的 `logs.Errorf`**，如 `backend/api/middleware/session.go:57,65,82,90`，这类日志连 log-id 都没有。
- 级别由环境变量 `LOG_LEVEL` 控制，默认 Info：`backend/main.go:129-151`（`setLogLevel`）。
- log-id 的注入：`backend/api/middleware/log.go:85-93` `SetLogIDMW` 生成 UUID 放入 ctx（`consts.CtxLogIDKey`）并写响应头 `X-Log-ID`。
- 中间件装配顺序见 `backend/main.go:93-101`：ContextCacheMW → RequestInspectorMW → SetHostMW → SetLogIDMW → CORS → AccessLogMW → OpenapiAuthMW → SessionAuthMW → I18nMW。

**调用规模**（`grep -c`，剔除 _test）：Error 家族（`CtxErrorf|Errorf`）530 处；Warn 家族 164 处；Info/Notice 144 处；`Notice` 级仅 5 处使用。

## b) 错误体系

**`backend/pkg/errorx`：code+msg 的 StatusError，创建时捕获字符串栈。**

- `backend/pkg/errorx/error.go:30-36`：`StatusError` 接口 = `error` + `Code() int32` + `Msg()` + `IsAffectStability()` + `Extra()`。
- `backend/pkg/errorx/internal/status.go:136-173`：`NewByCode`/`WrapByCode` 构造 `withStatus{status, cause, stack}`；`WrapByCode` 保留 cause 并支持 `Unwrap/Is/As`（:70-90）。栈在**创建/包装点**用 `runtime.Callers` 抓取并转成字符串（`backend/pkg/errorx/internal/stack.go:44-60`），`Error()` 的输出是 `code=… message=…\ncause=…\nstack=…`（`status.go:96-111`）——即**栈以文本形式内嵌在 error 字符串里**，任何 `%v` 打印都会带出多行栈。
- 错误码注册：`backend/pkg/errorx/internal/register.go:43-55` `Register(code, msg, opts...)`，码表按域拆在 `backend/types/errno/*.go`（如 `user.go:24-35` 定义 700000001~700012006 段，`init()` 里 `code.Register`）。业务错误普遍 `code.WithAffectStability(false)`（`errno/user.go:42,48`）。
- 业务/系统错误的类型区分：**不是靠 Go 类型，而是靠是否包了 StatusError**。包了 → 业务错误（HTTP 200 + body code）；没包的裸错误（gorm、fmt.Errorf 等）→ 一律按系统错误 500。`IsAffectStability` 在后端代码里**没有任何消费方**（grep 仅命中定义与注册），推测只用于前端/运维侧。
- 传输层转换：`backend/api/internal/httputil/error_resp.go:35-54`：
  - `BadRequest`（bind 校验失败）：400 + `{code:400, msg}`，**不打日志**（:35-37）。
  - `InternalError`：`errors.As(err, &StatusError)` 且 code≠0 → `logs.CtxWarnf("[ErrorX] error: %v %v", code, err)` + **HTTP 200** `{code, msg}`（:46-49）；否则 `logs.CtxErrorf("[InternalError] error: %v", err)` + **500** `"internal server error"`（:52-53）。
- 调用方式：生成的 handler 模板固定 `if err != nil { internalServerErrorResponse(ctx, c, err); return }`，如 `backend/api/handler/coze/conversation_service.go:49-52`、`passport_service.go:100-102`、`knowledge_service.go:104-108`。

## c) 记录位点（核心问题）

**总体：没有统一规则。application/domain 里既有纯传播也有 log-and-return；边界有强制兜底；因此重复记录普遍存在。** 以下三条完整调用链：

### 链 1：conversation ClearConversationHistory（传播链，边界兜底）

| 层 | 文件:行 | 行为 | 打日志? |
|---|---|---|---|
| handler | `backend/api/handler/coze/conversation_service.go:49-52` | `application.ConversationSVC.ClearHistory` 出错 → `internalServerErrorResponse` | 交给 InternalError（Warn 或 Error） |
| application | `backend/application/conversation/conversation.go:59-95` | 纯传播 `return resp, err`；`currentRes == nil` 时 `errorx.New(errno.ErrConversationNotFound)`（:69-71） | 不打 |
| domain service | `backend/domain/conversation/conversation/service/conversation_impl.go:58-68` | 纯传播 | 不打 |
| DAL（gorm） | `backend/domain/conversation/conversation/internal/dal/dao.go:67-78` | `gorm.ErrRecordNotFound` → `return nil, nil`（静默）；其他错误裸 `return nil, err` | 不打 |
| 边界 InternalError | `backend/api/internal/httputil/error_resp.go:43-54` | StatusError → Warn + 200；裸错误 → Error + 500 | 打 1 次 |
| AccessLogMW | `backend/api/middleware/log.go:58-81` | 按响应状态码再打一行：200→Info，500→Error | 再打 1 次 |

一条裸 DB 错误在此链产生 **2 行 Error**（InternalError + AccessLog）；一个"conversation 不存在"业务错误产生 1 行 Warn（[ErrorX]）+ 1 行 Info（访问日志，状态 200）。

### 链 2：passport 登录 PassportWebEmailLoginPost（业务拒绝静默映射）

- handler：`backend/api/handler/coze/passport_service.go:90-114`，bind 失败直接 `c.String(400)` 不打日志；业务错误 → InternalError。
- application：`backend/application/user/user.go:54-91`，`errorx.New(ErrUserInvalidParamCode)`（:59）、`errorx.New(ErrNotAllowedRegisterCode)`（:69）均**创建即返回、不打日志**；其余纯传播。
- domain：`backend/domain/user/service/user_impl.go:67-110` `Login`：`!exist` → `errorx.New(errno.ErrUserInfoInvalidateCode)`（:73-75，**密码错/用户不存在完全静默**，靠边界 Warn 记录）；repo 错误裸传播或 `fmt.Errorf("…: %w", err)` 包装（:88, :285, :309, :331——用户表插入失败变成 `insert user failed: %w`，仍不打日志）。
- DAL：`backend/domain/user/internal/dal/user.go:40-42`，`ErrRecordNotFound` → `(nil, false, nil)` 静默。
- 附加位点：`SessionAuthMW`（`backend/api/middleware/session.go:63-67`）校验会话失败时 `logs.Errorf("[SessionAuthMW] validate session failed")`（**Error 级、且不带 ctx**），随后又调 `InternalError` 再记一次——**同一错误 2 行日志、级别还不一致（Error + Warn）**。

### 链 3：knowledge DeleteKnowledge（log-and-return 链，三重 Error）

- application：`backend/application/knowledge/knowledge.go:65-67`：`logs.CtxErrorf(ctx, "delete knowledge failed, err: %v", err)` 然后 `return err`——**log-and-return，错误继续传播**。同模式还有 `knowledge.go:80-82, 106-108, 124-126`、`convertor.go:627-629`。
- domain：`backend/domain/knowledge/processor/impl/custom_table.go:36-56, 90-91`：`CtxErrorf` + `errorx.New(errno.ErrKnowledgeDBCode, …)` 再返回。
- handler：`backend/api/handler/coze/knowledge_service.go:104-108` → InternalError → **Error + 500**。
- AccessLogMW：500 → **再一行 Error**。
- 一条 DB 失败共 **3 行 Error**；若 gorm 默认 logger（`gorm.Open` 未配置 logger，见 `backend/infra/orm/impl/mysql/mysql.go:33`）也打印了出错 SQL，则是第 4 行。

**log-and-return 的规模**：在 domain+application 中"`logs.CtxErrorf`/`CtxWarnf` 的下一行就是 `return`"的模式约 **281 处**（两行窗口扫描）。这正是 glex ADR 0007 第 3-7 行描述并否决的形态。

**HTTP 框架与 recover**：Hertz（`go.mod:33`）。`backend/main.go:84` 用 `server.Default`，其中注册了 hertz 内置 `recovery.Recovery()`；其默认 handler 打 `hlog.SystemLogger().CtxErrorf(c, "[Recovery] err=%v\nstack=%s", err, stack)` 并返回 500（hertz v0.10.2 源码 `pkg/app/middlewares/server/recovery/option.go:36`，`pkg/app/server/hertz.go:48-53`）。该日志走 hertz 自己的 hlog 通道（未被 `pkg/logs` 接管），recover 兜底**有**，但与业务日志不共享 log-id 等上下文。AccessLogMW 不是错误兜底（它只看状态码），真正的错误兜底是每个 handler 模板里的 `InternalError`。

## d) 级别区分

- **边界规则（唯一明确的规则）**：StatusError → Warn（HTTP 200 + code）；裸错误 → Error（500）。见 `error_resp.go:46-53`。
- **AccessLogMW 按状态码**：≥500 Error、≥400 Warn、其余 Info/Debug（`log.go:58-81`；Info 级还会打印 3KB 截断的请求/响应体）。注意业务错误响应是 200，所以在访问日志里只体现为 Info——业务拒绝的 Warn 记录只来自 `[ErrorX]` 那一行。
- **Error 被滥用为默认级别**：
  - 业务"not found"打 Error：`custom_table.go:40-41`（"table doc not found" → Error + `ErrKnowledgeDocumentNotExistCode`）、`local_table.go:43-44`。
  - 预期会话失效打 Error：`session.go:65`。
  - application 层把下游失败打 Error 后继续传播（链 3），此时它并不"处理"错误。
- **Warn 的正确用法也存在**：降级场景，如 `user_impl.go:363`（查重名失败 → 取 email 兜底 + CtxWarnf）、`session.go:100`（admin 邮箱未配置提示）。
- **业务拒绝（参数错/无权限/配额不足）在源头一律不打日志**，只在边界被 `[ErrorX]` 记为 Warn——这一点与 glex"业务拒绝至多 Warn"方向一致，但 coze-studio 是"靠边界兜底"而非约定。

## e) 豁免场景

- **gorm not-found 静默**：`dal/dao.go:70-72, 135-137, 171-173`、`domain/user/internal/dal/user.go:42` 等普遍把 `ErrRecordNotFound` 映射为 `nil,nil` 或 `exist=false`，不打日志、不产生错误。
- **redis.Nil**：`backend/infra/cache/impl/redis/redis.go:39` `cache.SetDefaultNilError(redis.Nil)`——缓存库把 redis.Nil 注册为"未命中"哨兵错误，调用方按 miss 处理，不当异常记录。
- **context.Canceled**：workflow 域显式识别并静默改道——`backend/domain/workflow/internal/nodes/batch/batch.go:408-409`（`context.Cause(ctx)` 为 Canceled → 返回 `context.Canceled`，注明"canceled by Eino workflow engine"）；`backend/domain/workflow/internal/execute/callback.go:372-388`（Canceled → 发 WorkflowCancel 事件）；`backend/domain/workflow/internal/compose/node_runner.go:828-834`（DeadlineExceeded/Canceled 映射为 NodeTimeoutErr/CancelErr，不走通用包装）。均不打日志。
- **bind 校验失败**：`invalidParamRequestResponse`/`c.String(400)` 不打日志（`base.go:27-29`、`error_resp.go:35-37`、`passport_service.go:94-96`），只有 AccessLogMW 的 400→Warn 一行。
- **静默吞错**也存在且无注释：`user_impl.go:413-416`（取头像 URL 失败 `continue` 跳过用户，无任何日志）。

## f) 成文约定

**没有日志策略文档。** 逐项检查：

- `CLAUDE.md`（仓库根）：Backend Development 一节仅有一句 "Implement proper error handling with custom error types"（CLAUDE.md:139），无日志规范。
- `CONTRIBUTING.md`、`README.md`、`backend/README.md`：grep 无 logging 相关内容。
- 唯一接近约定的是代码形状本身：handler 生成模板固定调 `internalServerErrorResponse`；`pkg/logs` 的 Ctx* API 形状暗示"带上 ctx 以获得 log-id"，但部分代码（session.go 等）并不遵守。

## g) 与 glex ADR 0007 对照

| 维度 | glex ADR 0007 | coze-studio 实际 |
|---|---|---|
| 记录次数 | 恰好一次，在处理位点；传播不打 | 无规则：domain/application 约 281 处 log-and-return，同一错误 2~4 行日志常见（链 3） |
| 边界兜底 | 传输层 logging.Server 记录到达边界的错误 | 有且更强：handler 模板强制 `InternalError`（Warn/Error）+ AccessLogMW 按状态码 + hertz recover |
| Infra 边界 | log-and-map，裸驱动错误不出边界，返回领域 sentinel | **无**：裸 gorm 错误一路传到 handler 变成笼统 500（链 1）；eventbus 消费者是例外（在自己的处理位点打，`infra/eventbus/impl/rmq/consumer.go:92`） |
| 预期条件 | `ent.IsNotFound` → sentinel，静默映射 | 一致思路：`gorm.ErrRecordNotFound` → `nil,nil`/`exist=false`；业务错误 `errorx.New` 静默创建（链 2） |
| 豁免 | context.Canceled、redis.Nil、sentinel not-found | 一致：redis.Nil 设为 miss 哨兵（`redis.go:39`）；Canceled 映射为取消事件/专用错误（workflow 域）；not-found 静默 |
| 级别 | Error=系统失败；业务拒绝至多 Warn | 边界一致（StatusError→Warn）；但 Error 是事实上的默认级，业务 not-found、会话失效都打 Error |
| 结构化字段 | 强制 `operation`（点路径）+ `slog.Any("error", err)` | 无结构化：纯文本 printf，错误经 `%v` 拼进消息；唯一 ctx 字段是 `[log-id]` 文本；部分日志不带 ctx |
| 栈 | `AddSource` + 传输层栈，禁止手打栈 | errorx 在创建点把**文本栈内嵌进 `Error()` 字符串**，凡是 `%v` 打印都带多行栈（`internal/stack.go:44-60`、`status.go:96-111`） |
| 成文与执行 | ADR + review-based | 无成文约定，靠 handler 代码生成模板隐式保证边界兜底 |

**值得 glex 借鉴的点**

1. **边界统一转换点的强制力**：所有 handler 由 hertz 代码生成器产出、模板固定调 `httputil.InternalError`，使"每个错误必有边界记录 + 统一 HTTP 形态（业务错误 200+code / 系统错误 500）"成为结构性保证，不依赖开发者自觉。glex 的 logging.Server middleware 目标相同，coze-studio 的"生成模板兜底"是另一条可参考的执行路径。
2. **业务错误 HTTP 200 + body code 的契约**：`[ErrorX]` Warn 行成为所有业务拒绝的唯一可查询日志，粒度整齐（含 code），对告警噪声友好。
3. **log-id 关联**：UUID log-id 注入 ctx + `X-Log-ID` 响应头，让用户报障时能直接反查日志（`log.go:85-93`）——glex 目前无等价的对外关联标识（ADR 0007 只有 operation 字段）。
4. **redis.Nil/ErrRecordNotFound/Canceled 的豁免清单与 glex 高度一致**，可视为跨项目共识，佐证 ADR 0007 的豁免设计。

**需要警惕的点（反面教材，多数正是 ADR 0007 要解决的）**

1. **重复日志**：log-and-return + 边界兜底 + 访问日志三层叠加，一条 DB 失败 3~4 行 Error（链 3），ADR 0007 第 3-5 行预判的问题在 coze-studio 是现实。
2. **级别滥用**：Error 是默认级（530 处 vs Warn 164 处），业务 not-found、会话过期均打 Error，Error 告警语义已被稀释；且存在同一错误两行日志级别不一致（session.go:65 Error + InternalError Warn）。
3. **无结构化字段**：错误全部 printf 进消息文本，无 operation/error 字段，机器检索与告警聚合能力弱；不带 ctx 的 `logs.Errorf` 还会丢 log-id。
4. **裸错误穿透边界**：无 log-and-map，gorm 原生错误文本直达 handler 才被笼统化成 "internal server error"，原始 cause 只存在于边界日志里——客户端可见信息与日志排查完全耦合，这与 glex ADR 0004 的 errnormalize 动机相同，coze-studio 没做这一层。
5. **栈内嵌进 error 字符串**：`%v` 一次打印多行，污染日志解析；同时 `WrapByCode` 的"已有栈就不再抓"逻辑（`status.go:164-171`）说明作者也意识到栈会重复——glex 用 `AddSource` + 中间件栈的方案更干净。

**未能确认的问题**

- glex 侧最关心的"同一错误的多次日志是否同 trace 关联"：coze-studio 无 trace_id 体系（未见 OTel/skywalking 采样代码进日志），只有 log-id，无法进一步验证关联效果。
- `IsAffectStability` 的消费方：后端代码无消费，推测由前端按 code 判定提示策略，未在本次 sparse checkout（仅 backend/）范围内验证。
- hertz 内置 access logger（`server.Default` 未启用 access 中间件，仅自写 AccessLogMW）——已确认自写版本是唯一访问日志，无遗留疑问，但因 hertz 版本较新，未逐行核对 `Recovery()` 在 v0.10.2 之外的行为差异。
