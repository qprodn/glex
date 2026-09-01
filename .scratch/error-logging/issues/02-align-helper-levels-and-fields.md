# 02 - Align existing helpers: level Error, dotted operation

Status: resolved
Blocked by: 01

The existing log-and-map helpers log at `Warn` for what ADR 0007 classifies
as system failures. Raise them to `Error`:

- `internal/infra/authn/auth_login.go:184` ("authentication database
  operation failed")
- `internal/infra/authn/auth_storage.go:110` (`observe` — only the non-benign
  branch; `redis.Nil` / `ErrAuthKeyNotFound` stay silent)
- `internal/infra/access/menu.go:327` (`routeAccessError`)

At the same time, make every `operation` field a dotted path (e.g.
`authn.login_by_password`, `access.route_access_query`) so the field is
self-sufficient without the component logger. Keep `slog.Any("error", err)`
as-is; never printf the error into the message.

Rule: ADR 0007. Acceptance: `grep -rn "WarnContext\|logger.Warn" internal/infra`
shows no system-failure mappings left at Warn; `go build ./...` passes.

## Comments

Done. Raised the three named helpers to `Error` (`authDatabaseBackendError`,
`observe`'s non-benign branch, `routeAccessError`) plus the two further
system-failure mappings the acceptance grep surfaced:
`authRedisBackendError` (login_session.go) and the inline
`ResolveAuthorization` log (access/authorization.go). All now carry dotted
`operation` fields (`authn.reload_login_system_user`,
`authn.storage_set`, `access.load_route_menu_grants`,
`access.resolve_authorization`, …); `slog.Any("error", err)` untouched.
The remaining Warn sites are deliberate best-effort degradation logs
(locker release/renew, store close-on-shutdown) — not error mappings, so
they stay at Warn per ADR 0007, now carrying dotted `operation` fields
too. Code review additionally: (1) gave every raised mapping site the
`context.Canceled` silent-exemption ADR 0007 requires (the shape
`accessStoreError` already had), and (2) raised the locker's "lock lost
during critical section" to `Error` with `lock.lock_lost` — it maps to
`ErrLockLost`, so it is a system-failure mapping, not degradation.
`go build ./...` and the full test suite pass.
