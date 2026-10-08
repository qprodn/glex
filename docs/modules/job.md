# job 模块指南：设计、使用与外部调度器迁移

job 模块是 glex 的第八个模块（ADR 0023），为后台提供**定时任务**能力：运营人员在控制台上配置"什么时间、以什么参数、触发哪段代码"，系统按 cron 计划反复执行并留下运行历史。它对标 RuoYi 的定时任务页面，但为 Go 原生设计——没有 Quartz、没有 `invoke_target` 反射字符串。

本文面向后续接手本模块的开发者，分三层展开：**它为什么这样设计**（第 2–4 章）、**日常怎么用**（第 5 章）、**将来换成 xxl-job 等外部调度器时改哪里**（第 6 章）。

相关文档：[ADR 0023](../adr/0023-job-scheduler-in-process.md)（设计决策全文）、[ADR 0003](../adr/0003-redis-distributed-lock.md)（分布式锁）、`GLOSSARY.md` 的 job 术语节。

## 1. 核心概念

三个概念构成整个模块的词汇表（术语与 `GLOSSARY.md` 一致）：

| 概念 | 中文 | 本质 | 存活处 |
|---|---|---|---|
| **Job Executor** | 任务执行器 | 一个 Go 类型，实现 `Name() string` + `Run(ctx, args) (string, error)` | **代码**，编译期注册 |
| **Scheduled Job** | 定时任务 | 一行数据：名字、handler 名、cron 表达式、参数、超时、开关 | **数据库 `job` 表** |
| **Job Log** | 任务日志 | 一次执行完成后的结果记录（成功/失败、摘要、耗时） | **数据库 `job_log` 表** |

关键分工：**执行器是代码，任务是数据行**。任务行只保存执行器的注册名（如 `pay.expire-sweep`），保存时对照进程内注册表校验——拼错名字的任务根本存不进去，操作员输入永远不会被反射执行。

## 2. 总体设计

### 2.1 分层与文件地图

模块遵循 glex 的标准四层（ADR 0005），另有一个刻意独立出来的运行时文件：

```
internal/domain/job/            领域层
├── executor.go                 Executor 接口 + Registry + RunLockKey —— 模块的"耐用资产"
├── errors.go                   类型化错误（sentinel）
├── entity/job.go               Job / JobLog 实体、Validate、Schedule 解析
├── repository/job.go           JobRepo / JobLogRepo 端口（接口）
└── service/job.go + provider.go  领域服务 Jobs：CRUD 校验、Fire（唯一执行路径）

internal/application/job/       应用层
└── job.go + provider.go        JobApplication：proto DTO 转换、控制台 API、手动执行

internal/infra/job/             基础设施层
├── job.go / job_log.go         ent 仓储实现（listq 查询、log-and-map 错误）
├── runtime.go                  ★ SchedulerRuntime：tick 循环 —— 唯一"可替换件"
├── snowflake.go                雪花 ID 生成
└── provider.go                 fx 装配（含 RegisterExecutors 汇聚点）

api/job/v1/job.proto            HTTP API 定义（/monitor/jobs、/monitor/job-logs…）
internal/infra/ent/schema/job.go  表结构（job、job_log）
```

依赖方向与全仓一致：application → domain service → repository 接口 ← infra 实现。特别注意两点：

- `Executor`/`Registry` 放在 `internal/domain/job` **包根**而不是 service 子包——这样 pay 等模块的 infra 适配器实现执行器时不需要 import 领域服务包（与 `internal/lock`、eventbus 契约同样的摆放逻辑）。
- `SchedulerRuntime`（`runtime.go`）是**一个文件大小的替换缝**：内置调度循环的全部逻辑都在这里，换外部调度器时它就是被换掉的部分（见第 6 章）。

### 2.2 五个核心设计决策

每个决策都回答一个"为什么不用更常见的做法"。

**① 执行器是代码，任务是数据行。** 执行器由拥有该业务的模块通过 fx value group（`group:"job-executors"`）在启动时注册进 `Registry`；任务行只引用注册名。重名注册直接 panic——那是装配 bug，不允许静默二选一。这杜绝了 RuoYi Quartz 时代的 `invoke_target` 字符串反射。

**② 对称副本 + 两把 Redis 锁，不做 leader 选举。** 每个副本都跑 tick 循环、独立计算到期任务，用两把锁（ADR 0003）代替选主：

- `job:fire:<id>:<epoch>`（epoch = 计划时刻的 unix 秒）——**occurrence 级声明**，fail-fast 抢占。没有这把锁，只靠运行锁时：一个快速跑完的副本释放了锁，晚 tick 的副本因为没记录过这个 occurrence 会把同一次触发再跑一遍。
- `job:run:<id>` ——**任务级不重叠**，持有整个运行期。上一个 fire 还在跑，调度侧跳过本次，手动执行返回 409 `JOB_BUSY`。手动执行走同一把锁，天然与调度执行互相去重。嵌套顺序固定（fire 外层、run 内层），无同 key 重入。

**③ 尽力而为触发，缺失不补偿。** 重启后从下一个未来时刻开始，**错过的触发永不回补**（宕机后的追赶风暴没有价值——锚点用例里懒结算路径本来就兜底）；执行器必须容忍同一次触发跑两遍（锁丢失会中途取消运行）和一次不跑。Redis 不可用时停止触发而不是重复触发。这与 eventbus 的投递纪律一致（ADR 0022）。

**④ 运行记录 write-once。** 每次执行完成落一行 `job_log`：触发来源（scheduled/manual/external-预留）、状态、摘要或错误（截断）、开始时间、耗时。没有"running"状态、没有更新路径——行只在完成时出现，这是审计流 write-once 精神（ADR 0012）在运行记录上的同款。`create_by = 0`：手动执行也是系统执行的。

**⑤ 外部替换是声明过的缝，不是意外。** 耐用资产是执行器注册表和 Fire 记账：`Name()/Run` 正是 SnailJob 的 `@JobExecutor(name=…)` 和 XXL-Job 命名 handler 的绑定形状。可替换件只有 tick 循环一个文件，`conf.Job.enabled` 是总开关——外部调度器接管那天，关掉开关即可，glex 业务代码零改动。

## 3. 数据模型

两张表都带标准审计字段 mixin（ADR 0002），都是软删除，无硬删。

### `job`（Scheduled Job）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | bigint | 雪花 ID，仓储 create 时生成 |
| `name` | varchar(64) | 显示名，活跃行内唯一（部分唯一索引 `WHERE deleted_at IS NULL`） |
| `handler` | varchar(128) | 执行器注册名，保存时对注册表校验 |
| `cron` | varchar(64) | 5 段 cron（robfig `ParseStandard` 语法，含 `@daily` 等描述符） |
| `args` | text | 透明参数，原样传给 `Run`，最长 4096 |
| `enabled` | bool | 暂停/恢复开关，默认 true |
| `timeout_seconds` | integer | 单次运行超时，1–3600；创建时把配置默认值固化进行，之后改配置不影响已有行 |
| `remark` | varchar(255) | 备注 |

索引：`name` 部分唯一（删除后释放名字）、`handler`（控制台过滤）。调度扫描 `ListEnabled` 走全量活跃集（后台规模就几十行），不加专用索引。

### `job_log`（运行记录）

| 字段 | 类型 | 说明 |
|---|---|---|
| `job_id` | bigint | 裸引用，无外键——任务删除后日志仍可读作历史 |
| `handler` | varchar(128) | 触发时刻的 handler 快照，之后改名不改写历史 |
| `trigger` | smallint | 1=scheduled，2=manual，3=external（预留，见第 6 章） |
| `status` | smallint | 1=成功，2=失败；无 running |
| `message` | text | 摘要或错误文本，按 rune 截断到 2000（防止截断劈开多字节字符导致入库失败） |
| `started_at` / `duration_ms` | | 开始时间与耗时 |

索引：`(job_id, created_at)`（单任务历史）、`created_at`（全任务最近列表，默认倒序）。

种子数据（`docs/db/seed.sql`）预置了锚点用例：`支付单过期扫描`，handler `pay.expire-sweep`，`* * * * *` 每分钟，超时 120s。

## 4. 运行机制

### 4.1 调度循环（SchedulerRuntime）

每个副本一个 goroutine（fx start hook 启动，stop 时取消并等在途运行收尾）：

1. **每 `tick_seconds`（默认 10s）** 调 `ListEnabled()` 拉取全部启用任务。扫描失败（DB/Redis 挂了）只 Warn 并跳过本 tick，下个 tick 重试——不发明降级模式，继承系统的可用性姿态。
2. **推导到期 occurrence。** 基准点取"行的 `updated_at`（编辑会重锚计划）"与"本副本上次已派发的时刻"的较晚者；从基准点用 cron 表逐个推导 `Next` 直到不晚于 now。时刻由 cron 表推导而非"now"，中等时钟偏移只会延迟触发，不会改变或复制 occurrence 的身份。
3. **宽限窗与永久跳过。** 距 now 超过 2×tick 的过期 occurrence（停机积压）永久跳过，不回补；宽限窗内的每个到期 occurrence 都派发——任务周期短于 tick 时不会静默少跑。
4. **时区唯一。** 所有表达式在 `conf.Job.timezone`（默认 Asia/Shanghai）求值，永不取进程本地时区——副本必须一致，且容器 TZ 环境变量不能让凌晨 2 点的任务漂移。时区名非法直接构造失败：这是部署错误，不是警告。

### 4.2 一次触发的完整链路

```
tick（每副本，每 10s）
 └─ ListEnabled()                          扫描启用行
     └─ dispatchDue()                      cron 表推导到期 occurrence（宽限窗过滤）
         └─ dispatch()                     异步 goroutine，长任务不饿死循环
             ├─ 抢 job:fire:<id>:<epoch>   ① occurrence 声明（busy = 别的副本已处理）
             ├─ jobs.Fire(trigger=Scheduled)
             │    ├─ 抢 job:run:<id>       ② 任务不重叠（busy = 跳过本次）
             │    ├─ Registry.Lookup(handler)   未注册 → 落"handler not registered"失败行
             │    ├─ ctx 超时 = 行 timeout_seconds（锁 TTL = 超时 + 30s，续租看门狗保活）
             │    ├─ executor.Run(ctx, args)    panic 被 recover 成失败行
             │    └─ 落 job_log 行（detached ctx + 3s 超时，运行被取消也留得住记录）
             └─ 持 fire 声明到 occurrence + 2×tick   ③ 宽限窗过去前不释放
```

③ 是个容易忽略的细节：fire 声明必须持有到该 occurrence 离开**所有**副本的宽限窗。若声明随运行结束就释放，晚 tick 的副本（其 `lastFired` 没记录过这次 occurrence）会重新抢到已释放的锁、把同一时刻再跑一遍。

### 4.3 手动执行（RunJob）

`POST /monitor/jobs/{id}/run` 的语义是"立即异步跑一次"：

- 先 fail-fast 探测运行锁，忙则同步返回 409 `JOB_BUSY`；不忙则在 detached、按超时+30s 限界的上下文上派发 `Fire(trigger=Manual)`，接口立刻返回，结果落 job_log。
- **允许对暂停的任务手动执行**——一次性查看不是改计划。
- 探测与派发之间若恰好撞上调度触发，由 Fire 自己的运行锁吸收（表现为本次手动执行被跳过，Warn 一条）。

### 4.4 故障与边界行为

| 场景 | 行为 |
|---|---|
| 进程重启 | 从下一个未来 occurrence 继续，错过的永不回补 |
| 运行中崩溃/锁丢失 | 该 occurrence 作废（失败行尽力落库）；续租看门狗丢失锁时会取消运行 ctx |
| Redis 不可用 | 触发暂停而非重复触发：锁获取失败统一按"忙"处理（调度侧跳过本 tick 并 Warn；手动执行探测返回 409，运行中续租失败则按 `LOCK_LOST` 取消并落失败行） |
| 滚动发布，旧副本缺新执行器 | 旧副本抢到声明，落 "handler not registered" 失败行——漂移可见，发布完自愈 |
| 任务删除/暂停 | 进行中的运行跑完；扫描不再看到该行，之后不再触发 |
| 手工改库把 cron 改坏 | 该任务 Warn 后停止调度，直到重新编辑 |

## 5. 使用指南

### 5.1 配置（`conf.Job`）

```yaml
job:
  enabled: true                  # 总开关。false = 停止内置调度，只剩手动执行（外部迁移的钥匙）
  tick_seconds: 10               # tick 间隔（触发粒度）。0 = 默认 10
  default_timeout_seconds: 60    # 建行时的默认超时，创建时固化进行。0 = 默认 60
  timezone: Asia/Shanghai        # 所有 cron 的求值时区。空 = 默认 Asia/Shanghai
```

注意触发延迟以 tick 为界而非 cron 粒度：10s tick 下，`* * * * *` 的任务在整分钟后 ~10s 内触发。

### 5.2 控制台 API

| 操作 | HTTP | 说明 |
|---|---|---|
| 建任务 | `POST /monitor/jobs` | handler/cron/超时全量校验；`enabled` 不传默认 true |
| 查/列表 | `GET /monitor/jobs[/{id}]` | AIP-160 过滤（`name`/`handler`/`enabled`）、排序、分页 |
| 改任务 | `PATCH /monitor/jobs/{id}` | field-mask 局部更新；**enabled 不在此改** |
| 删除 | `DELETE /monitor/jobs/{id}` | 软删；在途运行跑完 |
| 暂停/恢复 | `POST /monitor/jobs/{id}/enabled` | 独立开关，不与编辑混在一起 |
| 立即执行 | `POST /monitor/jobs/{id}/run` | 异步；忙则 409 |
| 日志 | `GET /monitor/job-logs[/{id}]` | 过滤 `job_id`/`handler`/`trigger`/`status`/`created_at`，默认倒序 |
| 删日志 | `DELETE /monitor/job-logs/{id}` | 清理历史，不是改记录 |
| 执行器列表 | `GET /monitor/job-executors` | 已注册执行器名，前端 handler 选择器的数据源 |

### 5.3 新增一个执行器（开发者步骤）

以现存的 `pay.expire-sweep` 为完整范例（`internal/infra/pay/sweep.go`）：

**第 1 步：实现 `job.Executor`**（放在拥有该业务的模块的 infra 里）：

```go
type demoExecutor struct {
    demos demoservice.Demo // 依赖该模块的领域服务
}

func (e *demoExecutor) Name() string { return "demo.cleanup" } // 模块.动作，全局唯一
func (e *demoExecutor) Run(ctx context.Context, args string) (string, error) {
    n, err := e.demos.Cleanup(ctx, args)
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("cleaned %d rows", n), nil // 摘要会落 job_log.message
}
```

**第 2 步：在该模块的 fx Module 里挂进 value group**：

```go
var Module = fx.Module("demo-infra",
    fx.Provide(
        // 既有 provider…
        fx.Annotate(NewDemoExecutor, fx.ResultTags(`group:"job-executors"`)),
    ),
)
```

**第 3 步：确认模块已注册进 `cmd/glex/app.go`**（通常已注册）。`jobinfra.Module` 的 `RegisterExecutors` invoke 会在启动时把整个 group 灌进 `Registry`——不用改 job 模块任何代码。

**第 4 步（可选）：seed 一行任务**，或在控制台手工建。执行器随后即可在 `/monitor/job-executors` 看到。

**写 Run 的三条纪律**（ADR 0023 的后果，务必遵守）：

1. **容忍跑两遍**：重复触发是正常运作。要么天然幂等（像过期扫描那样每次从数据行重新推导候选），要么走 CAS 的领域路径收敛——绝不依赖"job 模块保证只跑一次"。
2. **容忍一次不跑**：缺失触发也是正常运作，任务只做收敛加速，不做唯一正确性来源。
3. **尊重 ctx 取消**：超时和锁丢失都会取消上下文，长循环要在 ctx.Done 时及时返回。

### 5.4 错误码

| 错误 | HTTP | 含义 |
|---|---|---|
| `JOB_NOT_FOUND` / `JOB_LOG_NOT_FOUND` | 404 | 不存在或已软删 |
| `JOB_NAME_CONFLICT` | 409 | 活跃任务重名（部分唯一索引兜底服务层的友好预检） |
| `JOB_BUSY` | 409 | 该任务已有运行在途 |
| `JOB_INVALID` | 400 | 名字空/超长，超时不在 1–3600，参数超 4096 |
| `JOB_CRON_INVALID` | 400 | 不是合法 5 段表达式 |
| `JOB_HANDLER_UNKNOWN` | 400 | handler 不是已注册执行器 |
| `JOB_STORE_UNAVAILABLE` | 503 | 存储层意外故障（infra log-and-map，ADR 0007） |

## 6. 迁移到外部调度器（xxl-job / SnailJob）

当真的需要调度中心（多语言执行器、工作流、告警面板、失败重试策略）时，迁移路径已在设计里预留。ADR 0023 的判断仍然成立：单服务后台引入第二个服务器是重决策，**由真实触发点驱动，而不是提前搬迁**。

### 6.1 替换缝在哪里

- **不换的（耐用资产）**：`Executor` 接口与 `Registry`——`Name()/Run(ctx, args)` 与 XXL-Job 的命名 handler、SnailJob 的 `@JobExecutor(name=…)` 是同一个绑定形状；`Fire` 的记账（不重叠、超时、panic 恢复、job_log 落库）。
- **换掉的（一个文件）**：`internal/infra/job/runtime.go` 的 tick 循环及其 fire 声明锁。
- **总开关**：`conf.Job.enabled = false` 停掉内置循环，glex 其余代码零改动。

### 6.2 两种接入形态

**形态 A：执行器客户端回调（xxl-job 原生形态，推荐）**

glex 进程内嵌一个 xxl 执行器客户端（Go 社区库如 `xxl-job-executor-go`），启动时遍历 `Registry` 把每个执行器注册为 xxl handler，桥接函数把 xxl 的参数原样传给 `Run`、把返回的摘要回传给调度中心。调度中心负责 cron 计算与触发时机。

**形态 B：鉴权触发端点**

glex 暴露一个内部触发端点（HTTP/gRPC，语义等同 RunJob，但 `trigger=TriggerExternal`——枚举值 3 已在表和 proto 里预留）。xxl 侧用通用 HTTP 任务或脚本调用它。改动更小，但参数传递与回执都不如形态 A 顺。

### 6.3 改造步骤（以形态 A 为例）

1. **部署 xxl-job-admin**，为 glex 建执行器配置（appname、地址）。
2. **新建适配器** `internal/infra/job/xxljob/`：fx 生命周期内启动/停止执行器客户端；一个桥接函数把 `Registry.Names()` 逐个注册为 xxl handler（xxl param → `Run` 的 args；返回 summary 作为执行日志）。
3. **决定调度真相源**。推荐：计划配置整体迁到 xxl 控制台，glex 的任务 CRUD 面退役（可先只读保留）。不推荐两边同时管 cron——一个计划两个真相源必然漂移。
4. **切开关**：`conf.job.enabled = false` 停内置循环。注意**切换是排他的**：内置循环的 fire 声明锁只在内置路径里，两个调度器同时在线会各跑各的（运行锁能挡住重叠，但日志行会翻倍且 trigger 混杂）——先关旧再开新，回滚则反向。
5. **触发路径尽量仍走 `Fire(trigger=TriggerExternal)`**：不重叠锁、超时、panic 恢复、job_log 历史全部白得，glex 控制台的日志页继续可用。
6. **xxl 侧策略对齐内置语义**（见 6.5 的对照表），尤其阻塞策略选"丢弃后续调度"。
7. **观察期后清理**：停用种子任务行；如确定不回滚，`runtime.go` 与 fire 声明锁可删。

### 6.4 保留与退役清单

| 组件 | 迁移后 |
|---|---|
| `Executor` 接口、`Registry`、`job-executors` group | **保留**，所有执行器零改动 |
| `Fire`（运行锁、超时、panic 恢复、记账） | **保留**，作为触发端点/桥接的落点 |
| `job_log` 表与日志查询/删除 API | **保留**，历史与控制台继续可用 |
| 手动执行 RunJob | **保留**（或由 xxl 控制台的"执行一次"取代） |
| `runtime.go` tick 循环、`job:fire:*` 声明锁 | **退役**（开关关闭即停用，后续可删） |
| 任务 CRUD 控制台（取决于 6.3 第 3 步的决策） | 退役或转只读 |

### 6.5 语义对照与差异

内置调度与 xxl-job 的概念映射，以及搬迁时必须主动对齐的差异：

| 维度 | glex 内置 | xxl-job | 迁移动作 |
|---|---|---|---|
| 任务定义 | `job` 行（handler 名 + cron + args） | 调度中心任务（JobHandler 名 + cron + 参数） | 一一对应，手工搬运 |
| handler 绑定 | `Registry` 按名注册 | 执行器按名注册 | 桥接层自动完成 |
| 时区 | `conf.Job.timezone`，全局唯一 | 调度中心时区 | 确认 admin 时区一致 |
| 并发/不重叠 | 运行锁硬保证，忙则跳过 | 阻塞处理策略（单机串行/丢弃后续/覆盖） | 选**丢弃后续**匹配内置语义 |
| 错过触发 | 永不回补 | misfire 策略可配（立即触发/忽略/二次确认） | 保持"忽略"，别开补偿 |
| 失败重试 | 无（靠下个周期收敛） | 可配次数 | 可开，但执行器必须已满足幂等纪律 |
| 超时 | 行内 1–3600s | 任务级超时 | 走 Fire 则行内超时仍生效 |
| 运行日志 | `job_log` 行，write-once | 调度中心执行日志（+ 走 Fire 则两者都有） | 无冲突 |
| 触发来源标记 | scheduled / manual / **external(预留)** | — | 外部触发统一落 `TriggerExternal` |

一句话总结：**执行器和记账是资产，循环是耗材**。换调度器换的是耗材。

## 7. 附录：关键文件索引

| 文件 | 内容 |
|---|---|
| `docs/adr/0023-job-scheduler-in-process.md` | 设计决策全文（本模块的"为什么"权威来源） |
| `internal/domain/job/executor.go` | Executor 契约、Registry、RunLockKey |
| `internal/domain/job/service/job.go` | 领域服务：CRUD 校验 + Fire |
| `internal/infra/job/runtime.go` | 调度循环（替换缝）+ RegisterExecutors 汇聚 |
| `internal/infra/job/provider.go` | fx 装配；`anchorRuntime` 强制构造否则循环不启动 |
| `internal/infra/pay/sweep.go` | 执行器实现范例 |
| `api/job/v1/job.proto` | API 面 |
| `internal/infra/ent/schema/job.go` | 表结构 |
| `docs/db/seed.sql` | 种子任务行 |
