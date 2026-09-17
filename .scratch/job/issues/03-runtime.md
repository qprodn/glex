# 03 调度 runtime:tick 循环、fire 认领锁、fx lifecycle

状态:todo — 依赖 02

- `internal/infra/job/runtime.go`:
  - fx Lifecycle 启停(镜像 eventbus NewBus:OnStart 起 goroutine,
    OnStop cancel + wg.Wait)。
  - 每 tick:`ListEnabled` → 对每任务从 basis(内存 lastFired 与行
    updated_at 取大)算 cron 后继,advance-to-future 到 ≥now;到即
    `WithLock("job:fire:<id>:<epoch>", → svc.Fire(scheduled))`,
    Busy 跳过、认领即记 lastFired。
  - 时区固定 conf.Job.timezone;tick/now 可注入(测试)。
  - conf.Job.enabled=false 时不起循环。
- provider Module 增 runtime 提供 + `fx.Invoke(RegisterExecutors)`
  (消费 `group:"job-executors"` 填 Registry,镜像 eventbus
  RegisterSubscriptions)。
- 测试:假时钟 + fake svc;覆盖到期触发、advance-to-future 不补跑、
  Busy 跳过、edited 任务重算 basis、Stop 收敛。多副本语义以
  `job:fire` 键设计评审 + miniredis 抽测。

验收:go test ./internal/infra/job/... 绿;go run 冒烟可见循环日志。
