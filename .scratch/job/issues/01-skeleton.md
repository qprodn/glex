# 01 job 模块骨架:两张表 + 执行器注册表 + fx 装配

状态:done

- `internal/infra/ent/schema/job.go` + `job_log.go`(审计 mixin、雪花、
  软删;job.name 活跃内唯一、handler 索引;job_log (job_id,created_at)
  活跃内 + created_at),`make generate`。
- `internal/domain/job`:errors 哨兵、`executor.go`(Executor 契约 +
  Registry,重名 panic)、entity(Job/JobLog + Trigger/RunStatus)、
  repository 双接口 + list options。
- `internal/infra/job`:双 repo(log-and-map + snowflake)+ Module。
- `conf.proto` 增 `Job job = 7`(enabled/tick_seconds/
  default_timeout_seconds/timezone),`make config`;config.yaml `job:` 段。
- app.go 挂 jobinfra.Module;main.go Supply bc.Job;go.mod 加 robfig/cron/v3。
- docs/db/schema.sql 同步 DDL;ADR 0023;CONTEXT.md job 语言;本目录文档。

验收:go build/vet/test 全绿;`go run ./cmd/glex` fx 图通过、迁移建表。
