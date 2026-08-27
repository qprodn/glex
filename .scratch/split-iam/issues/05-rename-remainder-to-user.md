# 05 — Rename the remaining iam packages to user

**What to build:** Rename what remains of iam to the user module: System User
profiles, GetInfo, and the user-side orchestration (for example, changing a
password calls authn's credential capability plus revocation). Distribute the
shared plumbing that ticket 01 left in place: each module gets its own errors
and list-option helpers beside its code, and each application module owns its
transaction port — all three bound to the same infra transaction manager at
composition time. No generic shared package is created.

**Blocked by:** 04 — extract access.

**Status:** done

- [x] No iam-named packages remain; the user module contains only profile concerns plus its orchestration
- [x] Errors and list-option helpers are owned per module; no shared/common package was introduced
- [x] Each application module declares its own transaction port, all bound to the one infra transaction manager
- [x] Branch compiles and module-scoped tests pass

## Comments

Implemented on the `split-iam` integration branch as a git-tracked physical
rename (zero behavior change):

- **Domain** — `internal/domain/iam` → `internal/domain/user` (package
  `user`). It holds only System User profile concerns plus the user-side
  revocation policy vocabulary: `user.go` (User DO with request-input
  Password, `SessionRevocationFrom`, `CurrentUserInfo`, `UserUpdate`,
  `UserRepo`, the six user errors), `security_change.go`
  (`SessionRevocation`, constructors, `SystemUserSessionRevocation`), and the
  former `pagination.go` renamed to `list_option.go` — the last of ticket
  01's deliberately-shared plumbing, now owned beside its only consumer and
  named to match `domain/access/list_option.go` and
  `domain/authn/list_option.go`. authn and access already owned theirs, so
  every module now owns its errors and list-option helpers; no shared or
  common package exists.
- **Application** — `internal/application/iam` → `internal/application/user`
  (package `user`): `UserApplication` (CRUD + GetInfo from the Principal
  snapshot), `UserUsecase` (the orchestration the ticket names: password
  writes go through the injected authn `CredentialWriter` contract inside
  the same transaction, classified revocations through the `SessionRevoker`
  contract), the policy-rich `security_change.go` revocation helpers, and the
  module's own `Transaction` port — all three application modules
  (user/access/authn) now declare one and `cmd` binds each to the single
  `*store.TxManager`. The Usecase's component logger tag moved from
  `application.iam.user` to `application.user`, matching the
  `application.<module>.<concept>` convention of the sibling modules.
- **Infra** — `internal/infra/iam` → `internal/infra/user` (package
  `userinfra`): the user repo (alias `userdomain` for the domain package,
  since the ent `user` predicate package already occupies the bare `user`
  name), the snowflake node helper, provider set.
- **Composition & pins** — provider aggregates, `server` HTTP/gRPC
  constructors, `../../../cmd/glex/wire.go`, and the regenerated
  `wire_gen.go` follow the rename; the authn integration-test harness
  (env-gated real-store family) re-imports the user module's constructors.
  `TestModuleSplitBoundaries` now pins user as the third named module
  (`application/user` imports no authn application/infra and no access
  package). `make all` and the full test suite are green on the branch.

Code-review findings folded in: two stale iam-module comment references
were reworded (`application/authn/login.go`'s assignment-span pointer now
names access, its home since ticket 04; `application/access/role_test.go`'s
defaulting note now says "not in the domain"). The reviewers flagged the
Usecase component tag `application.user` matching the adapter's — kept
deliberately: adapter and Usecase of one concept share a tag (authn's login
pair does the same with `application.authn.login`), and the user module has
exactly one concept. The pre-existing adapter tags without a module segment
(`application.role`, `application.menu`, `application.online_session`) are
untouched.

Known interims unchanged from ticket 04, owned by later tickets: the user
adapter still serves `api/iam/user/v1` until ticket 06 splits the proto
trees; the user delete still hard-deletes Role Assignment links through the
ent `ClearRoles()` edge until a service split.
