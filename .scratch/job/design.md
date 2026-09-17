# job 模块设计(定时任务)

状态:实施中(2026-09-17 定稿,ADR 0023)。原生设计,非移植——树内
RuoYi-Vue-Plus 5.x 已移除 Quartz sys_job 换用外置 SnailJob,经典参照不在
树内,且 invoke_target 字符串反射不符合 Go 惯例。

## 决策摘要(详见 ADR 0023)

- **执行器是代码,任务是行**:Executor = `Name() + Run(ctx, args)`,
  fx `group:"job-executors"` 注册进 `job.Registry`(镜像 eventbus 订阅
  的装配模式);任务行只带注册名,保存时现场校验,行内永不携带可执行
  文本。重名注册 panic(装配 bug)。
- **cron**:5 字段,robfig/cron/v3 `ParseStandard` 只做解析;统一在
  `conf.Job.timezone`(默认 Asia/Shanghai)求值——多副本必须一致,取
  配置不取进程本地时区。触达时刻(epoch)由 cron 表推导,时钟偏移只
  影响延迟不影响去重。
- **多副本无选主**:每副本对称跑 tick 循环(默认 10s),两层 Redis 锁
  (ADR 0003):
  - `job:fire:<id>:<epoch>` 认领一次触达(fail-fast,Busy=他副本已处
    理,跳过)→ 同一触达跨副本 at-most-once。仅靠 run 锁不够:快跑完
    的副本释放后,晚 tick 的副本会把同次触达当到期重复执行。
  - `job:run:<id>` 运行期持有 → 同任务不重叠;调度撞上即跳过,手动
    撞上返回 409 JOB_BUSY(与 LOCK_BUSY 同形)。嵌套顺序固定
    (fire 外、run 内),无同键重入。
- **宕机不补跑**:重启 advance-to-future;崩溃在途的触达即漏掉。执行
  器容忍重复(锁续期失败 ctx 取消记失败)与丢失,同 ADR 0022 纪律。
  Redis 故障=暂停不重复(认领失败 Warn 跳过)。
- **Job Log**:完成时一次写入(trigger 1 调度/2 手动/3 外部预留、
  status、message 截断 2000、started_at、duration_ms);无 running 态、
  无更新路径;仅查询与撤下(ADR 0012 精神,无 transport 溯源,
  create_by 恒 0)。
- **手动立即执行**:允许对已停用任务发起;异步(分离 ctx+超时),
  API 立即返回,结果看日志。
- **外部替换缝**:注册表与 Fire 记账是耐久资产(Name/Run 即 SnailJob
  `@JobExecutor(name=…)` / XXL-Job 命名 handler 的绑定形状);
  `runtime.go` 一个文件可换;`conf.Job.enabled` 是内置循环开关。

## 表

- `job`:name(64, 活跃内唯一)、handler(128)、cron(64)、args(text)、
  enabled(默认 true)、timeout_seconds(integer, 1–3600, 建行时取配置
  默认固化)、remark;索引 handler(活跃内)、name 唯一(活跃内)。
- `job_log`:job_id(无外键)、handler(快照)、trigger、status、
  message、started_at、duration_ms;索引 (job_id, created_at) 活跃内、
  created_at。

## 配置(conf.Job,`job = 7`)

enabled(总开关)、tick_seconds(0→10)、default_timeout_seconds(0→60)、
timezone(空→Asia/Shanghai)。

## 锚定用例:pay 过期扫描

pay 域服务导出 `SweepExpired(ctx, limit)`——repo 新增
`ListPendingExpired(before, limit)`(partial 索引
`(expire_time) WHERE deleted_at IS NULL AND status=1`),逐行走既有
`settle`(复用 already-paid 竞态处理),单行失败 Warn 后继续;
`internal/infra/pay/sweep.go` 的 `pay.expire-sweep` 执行器包装之,
seed 一条 `* * * * *`、timeout 120 的任务行。懒结算路径保留——扫描是
增量收敛;经扫描落 PAID 仍不发 PaymentCompleted(维持现有契约)。

## API / 前端

- JobService:Create/Get/List/Update/Delete/SetJobEnabled/RunJob/
  ListJobLogs/GetJobLog/DeleteJobLog/ListJobExecutors(表单下拉用)。
- perms:`monitor:job:list|get|create|update|delete|run`、
  `monitor:joblog:list|get|delete`,与种子按钮 1:1;不进 operlog skip。
- 页面:`pages/monitor/job/`(列表+表单+启停+立即执行)、
  `pages/monitor/joblog/`(列表+撤下)。

## 提交序列

1. 骨架(表/注册表/装配/ADR/CONTEXT)
2. 域服务(CRUD 校验、Fire、日志)
3. 调度 runtime(tick、认领锁、lifecycle)
4. pay 扫描执行器(+种子任务)
5. API + 应用层(make api)
6. web 页面 + 菜单种子

## 明确不做(范围外)

- 补跑错过的触达;misfire/并发策略(由 skip + no-overlap 语义取代)
- 任务参数模板化、通知告警、依赖编排(工作流)
- 外部调度器接入(缝已留:trigger=external 预留、enabled 开关)
