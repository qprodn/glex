# 04 - Propagation breadcrumbs: log-and-return at domain/application

Status: resolved

Reverse ADR 0007's propagation-silence for the two orchestration layers per
ADR 0011: domain services and application services log-and-return — every
propagating `err != nil` logs one `Warn` breadcrumb (`logx.Propagate`,
fields `operation` + `error`), then returns the error unchanged.

- `pkg/logx.Propagate(ctx, logger, operation, err)` — Warn, exempts
  `context.Canceled`; operation names: application `<module>.<rpc_snake>`
  (e.g. `authn.login`), domain `<module>.svc.<method>` (e.g.
  `authn.svc.verify_password`).
- Domain services gain an injected `*slog.Logger` (`fx.Supply` already
  provides it); application services already hold one.
- Boundary: replace kratos `logging.Server` with
  `pkg/middleware/logging.Server` — same attributes; 4xx → `Warn`,
  `context.Canceled` → `Info`, 5xx/unknown → `Error` (ADR 0007's "business
  rejections never `Error`" finally holds at the boundary).
- Handling sites unchanged: infra log-and-map `Error` + silent expected
  mappings stay exactly ADR 0007.

## Comments

- 2026-09-03: implemented in the working tree (uncommitted). 187 breadcrumb
  sites across domain services (logger injected, component
  `domain.<module>.svc`) and application use cases (existing loggers);
  `pkg/logx.Propagate` + `pkg/middleware/logging.Server` (4xx→Warn,
  Canceled→Info, 5xx/unknown→Error with stack) wired into both HTTP and gRPC
  servers. `go build ./...` and `go test ./...` pass. Note: the hand-written
  file transfer routes (`internal/server/file_transfer.go`) sit outside the
  kratos middleware chain, so for uploads/downloads the application
  breadcrumbs are the only request-scoped error logs.

- 2026-09-03: raised from the coze-studio field study
  (`../research-coze-studio-error-logging.md`) — its log-and-return gives
  the failure path in logs, but at `Error` everywhere; glex adopts the
  pattern at `Warn` so `Error` keeps its ADR 0007 meaning.
