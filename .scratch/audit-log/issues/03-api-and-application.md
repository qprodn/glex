# 03 — api/audit/v1 protos + application service + wiring

Status: resolved

`api/audit/v1`: `oper_log.proto` (OperLogService: List/Get/DeleteOperLog,
`/monitor/oper-logs`), `login_log.proto` (LoginLogService:
List/DeleteLoginLog, `/monitor/login-logs`), `error_reason.proto`
(OPER_LOG_NOT_FOUND, LOGIN_LOG_NOT_FOUND). `make api` regenerates Go + web
TS clients + openapi.yaml in the same change.

`internal/application/audit`: one AuditApplication implementing both
servers; no Transaction (single-row writes, file-module precedent);
ADR 0011 breadcrumbs; AIP-160 filter / AIP-132 order / page-token like dict.
Default order created_at desc, id desc (newest first — a monitor read).

Wiring: fx modules in cmd/glex, `auth_routes.go` permission handles
(`monitor:operlog:*`, `monitor:loginlog:*`).

## Accept

- `go build ./...`; auth route table covers every new operation.

## Comments

- 2026-09-04: Implemented: api/audit/v1 oper_log.proto + login_log.proto (List/Get/Delete per aggregate, /monitor/oper_logs + /monitor/login_logs, OperStatus/LoginStatus enums) + error_reason.proto; make api regenerated Go pb/grpc/http, web TS clients, and openapi.yaml in the same change. AuditApplication implements both servers (no Transaction — single-row writes, ADR 0012); default order created_at desc, id desc. Wired: cmd/glex fx modules, auth_routes handles monitor:operlog:* / monitor:loginlog:*, HTTP + gRPC registration.
