# 02 — audit domain module + infra repositories

Status: resolved

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

## Comments

- 2026-09-04: Implemented: internal/domain/audit (entities with write-once Validate, sentinels incl. OPER_LOG_INVALID/LOGIN_LOG_INVALID for writer bugs, repos with the shared ListOption pattern, Audit service with ADR 0011 breadcrumbs) + internal/infra/audit (log-and-map auditStoreError, per-package snowflake node 1, fx Module). Service tests + component-logger pin pass.
