# 04 — Extract the access module

**What to build:** Move Roles, Menus, Menu Grants, Role Assignment (the link
storage and both of its endpoints, including the application side of the three
assignment RPCs), and Authorization Resolution into the access module across
all three layers. The resolution contract is published at
`internal/crossdomain/access` in the same anti-corruption shape as the
revocation contract (contract.go, model snapshot, impl adapter, mock, per ADR
0017); authn's login consumes the injected contract to fill the Authorization
Snapshot and never imports access domain types. iam shrinks to System User
concerns. When this ticket lands, role, menu, and assignment flows work
end-to-end on the branch, and login produces snapshots via the crossdomain
contract.

**Blocked by:** 03 — extract authn.

**Status:** done

- [x] Role Assignment's storage and both endpoints (by user and by role) live in access; user flows orchestrate through access
- [x] The Authorization Resolution contract lives at `internal/crossdomain/access` exposing only model snapshots; authn's login consumes it by injection, imports no access domain package, and derives no permissions itself
- [x] Role/menu/assignment flows pass their tests on the branch; branch compiles with no behavior change

## Comments

Implemented on the `split-iam` integration branch as a physical module
extraction with one new crossdomain seam:

- **Domain** — `internal/domain/access` owns `role.go` (Role, Menu Grant
  link shape, `RoleRepo`), `menu.go` (Menu, route access, `MenuRepo`),
  `role_assignment.go` (the former `auth.go`, renamed to its concept: Role
  Assignment, `UserRoleRepo`), `list_option.go` (access's own AIP filter/
  order helpers), and the new `authorization.go`: `AuthorizationResolution`
  (Role Keys + Functional Permissions) with the `AuthorizationResolver` port,
  the `SuperadminRoleKey` constant (the Role key CONTEXT.md names, now owned
  by access's domain instead of an infra const plus an application literal),
  and `ErrAuthorizationUnavailable`. The by-role endpoint's user projection
  is the new access-owned `RoleUser` snapshot (mirrors authn's `LoginAccount`
  precedent), so no domain package imports another; `UserRoleRepo.UserExists`
  answers the by-user endpoints' existence gate through the assignment
  storage's user edge, and access carries its own `ErrUserNotFound` with the
  identical reason string so client behavior is unchanged. iam keeps only
  `user.go`, `security_change.go`, and `pagination.go`.
- **Crossdomain seam** — `internal/crossdomain/access` publishes the
  `AuthorizationResolver` contract (`ResolveAuthorization(systemUserID)`
  returning the flat `model.AuthorizationResolution`), `impl/` wraps the
  domain port, and `accessmock` holds the mockgen double. The model carries
  only Role Keys and Functional Permissions: the Authorization Snapshot's
  carrier fields (System User id, name) are authn's own data, filled from the
  revalidated account.
- **Application** — `internal/application/access` owns the Role, Menu, and
  Role Assignment usecases and transport adapters (the assignment adapter
  keeps serving the still-unified proto `AuthService`; ticket 06 moves the
  RPCs), its own `Transaction` port, and a deliberately slimmer
  `security_change.go`: access's writes are always administrative, so its
  revocation helper folds ids into one flat `RevocationRequest` without the
  user module's self-service policy vocabulary. `RoleAssignmentUsecase` drops
  the `UserRepo` dependency for the `UserExists` gate; `ListRoleUsers`
  returns `RoleUser` snapshots converted by access's own `convertRoleUser`.
  iam's application keeps only the user span (`UserUsecase`,
  `UserApplication`, the policy-rich revocation helpers).
- **Login flip** — `LoginAccountStore.LoadSnapshot` is gone from
  `domain/authn`; `LoginUsecase` receives the injected
  `access.AuthorizationResolver` and fills the Authorization Snapshot inside
  the login transaction via `loadAuthorization` — login derives no
  permissions itself and imports no access domain package. Any resolution
  failure maps onto authn's own `ErrAuthBackendUnavailable`, preserving the
  503 client behavior the interim `LoadSnapshot` had. The resolution reads
  join the context-propagated login transaction (proven by the sqlmock
  ordering test, which now drives the full usecase with the real contract
  adapter over the mocked graph).
- **Infra** — `internal/infra/access` (package `accessinfra`) holds the role,
  menu, and user-role repos plus the new `authorization.go` implementing
  `AuthorizationResolver` (the ent queries and the superadmin
  `*:*:*`-only-through-exact-role-key folding moved verbatim from
  `infra/authn`'s interim snapshot loader; driver failures map onto
  `ErrAuthorizationUnavailable` at the storage seam). The route-loading
  failure mapping onto `authndomain.ErrAuthBackendUnavailable` moved with
  `LoadRouteAccess` as ticket 03 scheduled. iam's infra keeps the user repo;
  the snowflake node helper is duplicated per package (both keep node 1, the
  pre-existing single-instance posture).
- **Composition & pins** — server joins the auth service from
  `accessapp.RoleAssignmentApplication`; wire binds `accessapp.Transaction`
  to the one `store.TxManager` alongside iam/authn. Wire regenerated;
  `TestModuleSplitBoundaries` now pins all three directions (authn and
  access application packages import no other module's domain/application/
  infra; iam likewise). `make all` and the full unit suite are green on the
  branch; the env-gated real-store integration family compiles against the
  new seams (harness constructs the access usecases and the crossdomain
  resolver) and runs with ticket 07 before the merge.

Known interims accepted by this ticket: the assignment RPCs still live on
the unified `api/iam` AuthService and the adapters import `api/iam/...`
until ticket 06; `infra/access/menu.go` keeps mapping route-load failures
onto `authndomain.ErrAuthBackendUnavailable` (an infra→authn-domain import,
moved unchanged per ticket 03's note, flips in ticket 07's re-home);
`SelectUserRouteTree` in `domain/access/menu.go` remains the pre-existing
unused helper the usecase's inline route shaping duplicates; iam's user
delete still hard-deletes the user's Role Assignment links through the ent
`ClearRoles()` edge on the user row (unchanged from main — the tables are not
migrated, and deleting a user cascades its links by glossary ruling — but it
means iam's adapter writes access's link table until a service split);
`infra/authn/auth_login_test.go`'s ordering test drives the login usecase
from the infra package so the SQL-order proof spans the cross-module seam
(ticket 07 re-homes it with the whole-graph suites); the snowflake ID node
helper is duplicated per package (each mints IDs for its own tables only, so
per-package node-1 generators cannot collide within a key space — cross-table
id equality is meaningless — and the make-it-configurable note predates this
ticket).
