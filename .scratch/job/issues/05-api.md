# 05 job API + 应用层

状态:todo — 依赖 02(CRUD 就绪即可并行于 03/04)

- `api/job/v1/job.proto` + `error_reason.proto`:JobService
  (Create/Get/List/Update/Delete/SetJobEnabled/RunJob/ListJobLogs/
  GetJobLog/DeleteJobLog/ListJobExecutors),AIP 分页/过滤/排序对齐
  dict;REST 路径与 monitor 邻居对齐;`make api`。
- `internal/application/job`:JobApplication;RunJob 异步
  (context.WithoutCancel + 超时 goroutine 调 svc.Fire(manual)),
  Busy 冒泡 409;create/update 解析默认 timeout 固化进行。
- server/http.go 注册 + `auth_routes.go` perms
  (monitor:job:…、monitor:joblog:…);不进 operlog skip。
- 测试:应用层 fakes(域服务端口)、RunJob 异步与 Busy 传播。

验收:go build/vet/test 绿;openapi/TS 产物入库同提交。
