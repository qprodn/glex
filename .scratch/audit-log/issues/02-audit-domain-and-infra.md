# 02 — audit domain module + infra repositories

Status: claimed

`internal/domain/audit`: entities (OperLog, LoginLog — write-once, minimal
Validate), errors (`ErrOperLogNotFound`, `ErrLoginLogNotFound`,
`ErrAuditStoreUnavailable`), repositories (OperLogRepo, LoginLogRepo with the
shared ListOption pattern), and the `Audit` domain service owning the only
repository access (ADR 0005) with ADR 0011 breadcrumbs.

`internal/infra/audit`: ent-backed repos, log-and-map `auditStoreError`
(ADR 0007), per-package snowflake node, fx Module.

## Accept

- Domain service tests (list guards, create validation) +
  `logger_test.go` breadcrumb pin, matching the dict module.
