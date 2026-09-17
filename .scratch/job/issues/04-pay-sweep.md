# 04 pay 过期扫描执行器 + 种子任务

状态:todo — 依赖 03(执行器组已可装配)

- pay repo 增 `ListPendingExpired(ctx, before, limit)`(id 升序);
  `pay_payment` 增 partial 索引
  `(expire_time) WHERE deleted_at IS NULL AND status = 1`;
  schema 注释 "never by a cron" 改为懒结算+扫描共存。
- pay 域服务增 `SweepExpired(ctx, limit) (int, error)`:列出过期
  pending 逐个 settle(复用 already-paid 竞态),单行 Warn 后继续。
- `internal/infra/pay/sweep.go`:`ExpireSweepExecutor`
  (Name="pay.expire-sweep"),Module `fx.ResultTags
  (group:"job-executors")` 提供。
- docs/db/schema.sql 索引同步;seed.sql 种子任务行
  (pay.expire-sweep,`* * * * *`,timeout 120,enabled)。
- 测试:SweepExpired 域用例(fake repo 诚实 CAS)+ 执行器包装。

验收:go test ./internal/domain/pay/... ./internal/infra/pay/... 绿。
范围外:扫描落 PAID 不发 PaymentCompleted(维持契约,event.go 注释同步)。
