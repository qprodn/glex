# Audit logs: Oper Log & Login Log (监控日志)

Port RuoYi's `sys_oper_log` / `sys_logininfor` as the audit module: the
monitoring history of **who did what** (Oper Log, 操作日志) and **who signed
in, how, and when sessions ended** (Login Log, 登录日志). Fills the remaining
gap in the monitor page family (online sessions already exist).

## Decisions

- **New module `audit`**, two aggregates, RuoYi-style tables `sys_oper_log`
  and `sys_login_log` (the awkward RuoYi name `sys_logininfor` becomes
  `sys_login_log` — the file module's `sys_oss` → `sys_file` rename
  precedent). No edges; rows carry denormalized `user_name` plus
  `user_id` (0 = unauthenticated actor, e.g. a failed login attempt for a
  nonexistent account) so history survives account withdrawal.
- **Oper Log capture is a transport middleware** (`pkg/middleware/operlog`):
  after the handler returns it records every non-safe-method kratos
  operation except an explicit skip set (`authn` Login/Logout — those are
  Login Log events). The hand-written transfer routes run the same chain
  via `ctx.Middleware`, so UploadFile is covered for free. The middleware
  defines its own `Recorder` port; the composition root adapts the audit
  domain service into it (`authmw` authenticatorAdapter precedent — pkg
  never imports `internal/`). Request payloads are protojson-marshaled with
  secret keys (`password`, …) scrubbed.
- **Login Log capture lives in authn** (only it knows login outcomes):
  login success/failure, logout, admin 强退, self-revoke. authn reaches the
  audit module through a new crossdomain contract `audit.LoginLogWriter`
  (the `access.AuthorizationResolver` precedent), mocks via mockgen.
- **Best-effort, never fail the business operation** (ADR 0012): a failed
  audit write logs `Warn` at its handling site and is dropped; the insert
  runs on a detached, short-timeout context so a disconnected client cannot
  cancel its own audit trail.
- **Withdrawal, not truncation**: ADR 0002 forbids hard deletes, so RuoYi's
  清空 becomes singular soft `DeleteOperLog` / `DeleteLoginLog`; the page
  batch-deletes its selection. Rows are write-once — no update endpoint.
- **Permissions**: `monitor:operlog:list/query/remove`,
  `monitor:loginlog:list/remove`; seed gains the 监控 directory with 在线用户
  (previously unseeded), 操作日志, 登录日志 and their buttons.

## Scope

- `internal/infra/ent/schema` (+regen), `docs/db/schema.sql`, `docs/db/seed.sql`
- `internal/domain/audit`, `internal/infra/audit`, `internal/application/audit`
- `api/audit/v1` (OperLogService, LoginLogService, error_reason)
- `pkg/middleware/operlog`, `internal/crossdomain/audit`
- authn application hooks (login/logout/kickout), fx + auth_routes wiring
- web: generated services, `monitor/operlog` + `monitor/loginlog` pages,
  registry, locales, seed menus

## Out of scope

- Login location (IP→geo), export, RuoYi's 清空-all, retention/archiving.
- oper-log coverage for the hand-written DownloadFile (a safe-method read).
- The `errnormalize` middleware — dropped. ADR 0007 absorbed it (see its
  Consequences).
