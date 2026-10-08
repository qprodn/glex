# 文档瘦身：ADR 与 AGENTS.md —— 执行记录

执行日期：2026-09-21 · 状态：**已完成**（4 批全部执行）
方案依据：全仓引用扫描 + 逐文件通读（详见文末"取证方法"）

## 0. 结论

盘点前的预期是"删掉一批没用的 ADR"，实际结论相反：**26 份 ADR 里只有 1 份该删**（0004），
24 份被代码/前端/数据库注释实际引用。真正的病灶是三处**结构性错误**——编号重号、
外部模板残留、单向 supersede 链接。

执行后：ADR 从 26 份 → **25 份**（删 0004、S3 改号 0026），`docs/agents/` 从 3 份 → 2 份。
**编号唯一性、引用可解析性现已全部通过**，`go build ./...` 通过。

| 验收项 | 执行前 | 执行后 |
|---|---|---|
| ADR 编号重号 | 2 份共用 **0007** | **0** |
| 悬空引用（引用不存在的编号） | 0 | **1**（0007 中的 0004 退役说明，刻意保留） |
| 孤儿 ADR（有文件零引用） | 0001、0004 | **1**（0001，刻意保留为 background） |
| 畸形引用（如 `ADR 2023`） | 2 处 | **0** |

---

## 1. 删除清单

| 对象 | 理由 |
|---|---|
| `docs/agents/triage-labels.md`（整份） | 表格左右两列**逐字相同**，是未填写的外来模板。实测 `ready-for-agent` 用 10 次，其余 4 个角色全为 0 次 |
| `docs/agents/issue-tracker.md` 的 "Wayfinding operations" 节 | 面向不存在的 `/wayfinder` 技能；仓内 5 个特性目录**无一个 `map.md`** |
| `docs/agents/domain.md` 的多上下文段 | 引用 `CONTEXT-MAP.md`、`src/<context>/docs/adr/`、`/domain-modeling`、`/grill-with-docs` 等本仓不存在之物 |
| `docs/adr/0004-normalize-untyped-errors-at-transport.md` | 决策**从未实现**（`pkg/middleware/errnormalize` 不存在，全仓无 `INTERNAL_ERROR` / `recovery.WithHandler`），职责已被 0007 + 0011 取代，唯一引用者只有 0007 |

删除前先把三件有价值的东西搬了家，信息不丢：

- `ready-for-agent` 标签这一条真实信息 → 写入 `issue-tracker.md` 的 Conventions。
- 0004 的 panic 统一化待办（唯一未被覆盖的部分）→ 写入 **0007 的 Consequences**。
- 0004 的退役事实 → 在 0007 中留一条 tombstone，说明编号不再复用。

---

## 2. 修改清单

### 2.1 编号冲突解除（核心修复）

两个文件共用 0007 造成代码注释指向两个不同决策：

| 文件:行 | 原指向 | 现指向 |
|---|---|---|
| `internal/infra/file/provider.go:13` | 0007（歧义） | **0026** |
| `internal/infra/file/s3.go:58` | 0007（歧义） | **0026** |
| `internal/domain/file/storage/storage.go:2` | 0007（歧义） | **0026** |
| `internal/domain/file/storage/storage.go:46` | 0007（歧义） | **0026** |
| `internal/domain/file/service/file.go:51` | 0007（歧义） | **0026** |
| `internal/server/file_transfer.go:130` | 0007 | 0007（错误日志，不变） |

`docs/adr/0007-s3-protocol-aws-sdk-static-config.md` → `0026-s3-protocol-aws-sdk-static-config.md`。

**为何让 S3 让号**：错误日志的 0007 被 8 个包十余处引用，且 0011/0022/0023/0024 都叠在
它上面；S3 一份只被 file 模块引用。**为何追加为 0026 而不插入中间**：`docs/db/schema.sql`
有 50 处、`web/src` 有 100+ 处 ADR 引用，重排等于全仓改写注释，收益为零。

### 2.2 补齐单向 supersede 链接

| 文件 | 补入内容 |
|---|---|
| `docs/adr/0008-file-bytes-flow-through-service.md` | 头部加 `Partially superseded by ADR 0016` 引用块——0016 早已声明取代 0008 的一部分，0008 却对此一字未提 |
| `docs/adr/0024-listq-deep-module-for-list-queries.md` | Exemption 段末加 `Retired by ADR 0025`——0025 明确 retire 了该豁免，0024 没有回指 |

### 2.3 描述性修正

| 文件 | 修正 |
|---|---|
| `GLOSSARY.md` | 模块枚举 "six modules" → **"nine modules"**，补入遗漏的 **audit**（并说明它无对外 API 服务，由中间件与 authn 写入）；补入 `sys_oper_log` / `sys_logininfor` 两个 RuoYi 来源表名；把"第七/第八个模块"改为不绑死序号的表述 |
| `docs/adr/0005-...md` | handler 枚举标注为 **2026-08-28 快照**（当时只有 access/authn/user 三模块）。**规则本身未动**——它是引用密度最高的 ADR（130 处），且由三条 depguard 规则强制 |
| `AGENTS.md` | 每个 `##` 小节首行加 `_Authority: …_` 标注，让双写漂移可被审计。**各小节内容保留**：AGENTS.md 是 agent 每次任务都要加载的口袋规则，刻意与 ADR 双写 |
| `docs/adr/0001-single-tenant.md` | 加 `_Status: background — in force, but cited by nothing._` 标记，解释"零引用不等于失效" |

### 2.4 AGENTS.md 层级修复

`### Issue tracker` / `### Triage labels` / `### Domain docs` 原先**挂在 `## Commits and
security` 之下**，读起来像"提交与安全"的子章节。现：删掉 `Triage labels`（源文件已删），
其余两个提升为 `##`。最终结构为 9 个平级 `##`。

---

## 3. 计划外发现并一并修复（4 类）

执行过程中的扫描又捞出 4 类原方案未列出的问题，均已修复：

### 3.1 前端两处编号误写（真实缺陷）

```
web/src/pages/monitor/job/index.tsx:18      (ADR 2023)  →  (ADR 0023)
web/src/pages/monitor/joblog/index.tsx:28   (ADR 2023)  →  (ADR 0023)
```

`ADR 2023` 指向一个不存在的编号（读起来像年份而非 ADR）。全仓畸形引用扫描确认仅此两处。

### 3.2 S3 语义引用漏了 3 个位置

原方案只列了 5 处 Go 注释，实际还有：

| 位置 | 处理 |
|---|---|
| `internal/conf/conf.proto:108` | 改为 0026，随后 **`make config` 重新生成** `internal/conf/conf.pb.go`（遵守"不手改生成文件"） |
| `docs/db/schema.sql:261`（列注释） | 改为 0026 |
| `docs/db/schema.sql:284`（`comment on column`） | 改为 0026 |

注意：`schema.sql` 第 284 行是**已应用到数据库的列注释文本**。已部署的库仍带旧文案，
如需同步要手工再执行一次该 `comment on column`（纯文案，无功能影响）。

### 3.3 `.scratch/` 中 4 处指向已删 ADR 的过期引用

这些是历史上已完成特性的工作记录，但 `error-logging/spec.md` 的 "Out of scope" 列表
仍把 `errnormalize` 当作待办入口，读者会去找已不存在的 0004：

| 位置 | 处理 |
|---|---|
| `.scratch/error-logging/spec.md:25` | 标注为 dropped，指向 ADR 0007 |
| `.scratch/audit-log/spec.md:54` | 同上 |
| `.scratch/error-logging/issues/01-...md:40` | "ADR 0004 territory" → "the retired ADR 0004 covered" |
| `.scratch/error-logging/research-coze-studio-error-logging.md:138` | "glex ADR 0004" → "glex 已撤回的 ADR 0004" |

历史判断（谁在什么时间研究了什么）保持原样，只修悬空指针。

### 3.4 误报排除

`schema.sql` 与 `docs/modules/job.md` 各有形如 `ADR 0007` 的引用，但语义是**错误日志**
（infra log-and-map），不属于 S3 组，**未动**。同理 `api/pay/v1/error_reason.proto` 与
其生成的 `web/src/services/pay/v1/index.ts` 也正确。

---

## 4. 验收结果

```
[1] 悬空引用（引用但无文件）:  ✗ ADR 4      ← 仅 0007 中的退役说明，刻意保留
[2] 孤儿文件（有文件零引用）:  ! ADR 1      ← 刻意保留（0001 已标记 background）
[3] 重号:                      （空）
[4] docs/adr 文件总数:         25
[5] go build ./...             exit 0
```

产品面（代码 / 前端 / 数据库 / docs / CONTEXT / AGENTS）引用到的编号：
`2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26`。

---

## 5. 明确保留（看似无用，删了会出事）

- **ADR 0001 单租户**：产品面零引用，但这正是它的价值——它是"为什么每张表都没有
  `tenant_id`"的常设答案。没有它，下一个人会判定为遗漏并提议加列（每表加列 + 全局
  查询过滤器 + 数据回填）。已加 `_Status: background_` 说明，避免下次盘点又把它算进"没用的"。
- **ADR 0008 与 0016 并存**：0016 只取代 0008 的一句话，上传与代理下载仍生效并被
  `internal/server/file_transfer.go` 等引用。是演进，不是重复。
- **AGENTS.md 的 7 个规则小节**：与 ADR 双写是刻意设计（agent 的常驻规则 vs 决策的完整论证），
  本次只加权威源标注，不改内容。
- **ADR 0013/0014/0015 未合并**：三份同为 2026-09-04、同一模块，但合计被引用 40 处
  （含 web 6 处），合并收益仅是"少 3 个文件"，成本是全仓注释改写 + 丢失"一份 ADR 一个决策"
  的可检索性。

---

## 6. 后续建议（本次未做）

1. **`GLOSSARY.md` 缺 audit 术语节**：模块枚举已补 audit，但 Language 部分没有 audit 的
   词汇条目，而 "Oper Log" / "Login Log" 被 ADR 0012 与 AGENTS.md 使用。建议按现有格式
   补一节（本次只做删除与纠错，未新增术语，以免超出范围）。
2. **已部署数据库的 `sys_file.bucket` 列注释**仍写 `ADR 0007`，需手工执行一次
   `comment on column` 才能同步（纯文案）。
3. **编号 0004 永久空缺**：ADR 界惯例是不复用已退役编号，已在 0007 中记录原因。
4. **panic 统一化是唯一未落地项**：若决定不做，可从 0007 的 Consequences 中再移除。

---

## 取证方法

- 引用扫描覆盖 `internal/` `pkg/` `api/` `cmd/` `web/src/` `docs/` `GLOSSARY.md`
  `AGENTS.md` `.scratch/`，按编号归一化后统计。
- 逐文件通读 26 份 ADR、3 份 `docs/agents/`、`GLOSSARY.md`、`AGENTS.md`、`CLAUDE.md`。
- "未落地"判定以产物存在性为准：`pkg/middleware/errnormalize` 目录、`INTERNAL_ERROR`
  字面量、`recovery.WithHandler` 调用三者全无。
- **踩坑记录**：BSD `grep -E` 不支持 `\b`，首轮 "ADR 0001 零引用" 的统计因该问题得出了
  假结论，已改用无 `\b` 的写法复核全部结论。
