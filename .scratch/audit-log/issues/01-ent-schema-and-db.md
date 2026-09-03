# 01 — ent schemas: sys_oper_log & sys_login_log + DB DDL/seed

Status: claimed

Ent schemas `OperLog` and `LoginLog` (AuditMixin, no edges, table annotations
`sys_oper_log` / `sys_login_log`), regenerated code, plus the matching
hand-maintained `docs/db/schema.sql` DDL and `docs/db/seed.sql` monitor
menus (监控 directory + 在线用户/操作日志/登录日志 pages + buttons + role
grants — the seed currently has no monitor rows at all, so the existing
online page gets seeded here too).

Fields:

- `sys_oper_log`: operation, http_method, oper_user_id (0 = anonymous),
  oper_user_name, ipaddr, request (redacted JSON), status (0 success
  1 failure), reason, error_msg, latency_ms, request_id.
- `sys_login_log`: user_id, user_name, ipaddr, browser, os, status
  (0 success 1 failure), msg.

## Accept

- `go generate ./internal/infra/ent/...` clean; `go build ./...`.
- schema.sql/seed.sql stay idempotent (`on conflict do nothing`).
