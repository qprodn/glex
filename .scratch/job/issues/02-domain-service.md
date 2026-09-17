# 02 job 域服务:CRUD 校验、Fire(锁/超时/panic 恢复)、日志

状态:todo — 依赖 01

- `internal/domain/job/service/job.go`:`Jobs` 接口 + 实现。
  - CRUD:Create/Update 校验 name/timeout(ErrJobInvalid)、cron
    (ParseStandard→ErrJobCronInvalid)、handler 在册
    (→ErrJobHandlerUnknown)、name 唯一预检(→ErrJobNameConflict);
    SetEnabled;Delete;Get/List。
  - `Fire(ctx, job, trigger)`:run 锁 `job:run:<id>`(Busy→ErrJobBusy)
    → ctx 超时 → recover panic → Job Log 行(成功/失败,message 截断
    2000)。Log CRUD:Get/List/Delete。
  - 构造注入 Locker(ADR 0003)+ Registry + 双 repo + logger。
- `service/provider.go` Module(含 NewRegistry 提供)。
- 测试:fake 双 repo / fake locker(可编程 Busy)/ 假执行器;覆盖
  校验拒绝、Fire 成功/失败/panic/超时/Busy、日志落行字段。

验收:go test ./internal/domain/job/... 绿。
