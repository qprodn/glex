# 03 - Audit explicitly ignored errors (`_ = err`)

Status: resolved

Audit every explicit ignore in `internal/` and `pkg/` (~10+ sites, e.g.
`internal/infra/authn/auth_login.go:144-148` best-effort logout cleanup,
`internal/infra/access/snowflake.go:9` node init). Per ADR 0007 each site
gets one of:

- a comment justifying why the silence is safe, or
- a `Warn` log with `operation` + `slog.Any("error", err)` when the failure
  is worth knowing about (e.g. logout cleanup dropping sessions).

Rule: ADR 0007. Acceptance: no bare `_ =` on an error-bearing call without
an adjacent justification comment or a Warn; `go build ./...` passes.

## Comments

2026-09-01: Audited 13 sites (7 in `auth_login.go`, 4 snowflake node inits,
`internal/server/file_transfer.go:124`, `internal/application/authn/login.go:151`)
plus one `_ =` on a non-error (`internal/domain/authn/service/login.go:75`,
`VerifyPassword` returns a bool). Every site received an adjacent
justification comment; **no Warn was needed anywhere**, for one load-bearing
reason: issues 01/02 landed log-and-map at exactly the calls these sites
ignore (`sessions.Logout` → `authRedisBackendError`; `manager.LogoutByToken`/
`DeleteSession` → the `authStorage` `observe()` hook). A backend failure
therefore already logs `Error` at the adapter exit that owns it, and what
reaches the swallow sites is that exit's already-logged availability
sentinel plus benign leftovers (context cancellation, `ErrAuthKeyNotFound`
from already-gone keys, `ErrLoginSessionNotOwned` from a concurrent
revoke). A Warn at the swallow site would double-log every
failure — the exact shape ADR 0007's "logged exactly once" forbids. The
issue's "logout cleanup" Warn example was written against the pre-01 code,
where those exits were truly silent; the Warn intent is now satisfied
transitively by the deeper Error logs. The snowflake `NewNode` discards are
provably unreachable (the only failure is a node number outside 0–1023).
`go build ./...`, `go vet ./...`, and the full test suite pass.

