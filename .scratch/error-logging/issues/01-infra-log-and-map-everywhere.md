# 01 - Extend log-and-map to every infra exit

Status: resolved

Every exit path in `internal/infra/{user,dict}` (and any remaining raw
`return nil, err` exits in `access`/`authn`) maps raw ent/redis errors to
the module's domain sentinel instead of propagating them. Unexpected
failures log once at `Error` with `slog.Any("error", err)` and a dotted
`operation` field, then return the sentinel; expected business conditions
(`ent.IsNotFound` → e.g. `ErrUserNotFound`) map silently; `context.Canceled`
and `redis.Nil` are exempt.

Add missing sentinels following the existing exemplars
(`ErrAuthBackendUnavailable`, `ErrRouteAccessUnavailable`) and mirror the
log-and-map helpers: `internal/infra/authn/auth_storage.go:110` (`observe`),
`internal/infra/access/menu.go:327` (`routeAccessError`),
`internal/infra/authn/auth_login.go:184`.

Rule: ADR 0007. Acceptance: no raw ent/pgx/redis error value is returned
from any `internal/infra` adapter method; `go build ./...` passes; existing
tests green.

## Comments

Done. Added the missing 503 sentinels `ErrUserStoreUnavailable`,
`ErrDictStoreUnavailable`, `ErrAccessStoreUnavailable` (one per module,
following the exemplars) and mirrored the helpers as `userStoreError`,
`dictStoreError`, `accessStoreError` — these log `Error` with dotted
`operation` fields per this ticket, exempt `context.Canceled` from logging,
and still return the sentinel so no raw error crosses the boundary.

Every exit in `infra/user` and `infra/dict` now maps; the remaining raw
exits in `infra/access` (menu/role/user_role CRUD) and `infra/authn`
(`FindLoginAccount`, `StoreCredential`, and `loginSessionRepo.Create`'s
id-generation/encoding/corruption paths) are mapped too. The old helpers
(`authDatabaseBackendError`, `authRedisBackendError`, `routeAccessError`,
`observe`) are untouched — that is ticket 02. Known remaining non-ent exits:
startup config validation (`auth_manager.go`, `provider.go`), in-package
propagation into a mapping caller (`resolveAuthorization`, `findPO`,
`toBizLoginSession`), and client-input filter-expression errors (the territory
the retired ADR 0004 covered). `go build ./...` and the full test suite pass.
