# 03 — Extract the authn module onto the integration branch

**What to build:** Open the integration branch for the split and carve out the
authn module across all three layers: Credentials (hash, policy, verification),
Login Sessions and their tokens, Principal, the login/logout and
online-session usecases, and the sa-token machinery. The remaining iam code
reaches authn only through the crossdomain revocation contract and the
Principal context. Login's Authorization Snapshot filling may still resolve
authorization from the iam side — that flips in ticket 04. When this ticket
lands, login, logout, and online-session flows work end-to-end through the
whole stack on the branch.

**Blocked by:** 01 — single-concept file ownership; 02 — crossdomain revocation contract.

**Status:** done

- [x] Credential and Login Session ownership live in authn per ADR 0016; the credential column on the user record is read/written only by authn's adapter
- [x] Remaining iam code calls authn only via the crossdomain revocation contract and Principal
- [x] Login/logout/online-session flows pass their unit and infra tests on the branch; branch compiles with no behavior change

## Comments

Implemented on the `split-iam` integration branch as a physical module
extraction with one new seam:

- **Domain** — `internal/domain/authn` now owns `principal.go` (Principal,
  context bridge, `Authenticator`), `login_session.go` (Login Sessions,
  Authorization Snapshot, tokens, `LoginSessionRepo`), `auth_login.go`
  (login backend ports + login errors), plus two new files: `credential.go`
  (`HashPassword`/`VerifyPassword`/`DecoyCredentialHash` and the
  `CredentialRepo` port — the only writer of the credential column) and
  `list_option.go` (authn's own `ListOption` family). iam keeps its
  caller-side revocation vocabulary in `security_change.go`
  (`SessionRevocation`, constructors, `SystemUserSessionRevocation`), so no
  domain package imports another. `LoginSessionRepo.RevokeSystemUsers` was
  flattened to `(systemUserIDs []int64, exceptSessionID string)` so the port
  carries no foreign vocabulary; the crossdomain impl now translates the
  `model.RevocationRequest` directly onto it. `Revalidate` and the new
  pre-transaction `LoginAccountReader.FindLoginAccount` return the new
  authn-owned `LoginAccount` DO (id, name, hash, status) — login no longer
  reads iam's `UserRepo` at all.
- **Credential seam** — checklist item 1 moves the credential column behind
  authn now, and ticket 05 expects "changing a password calls authn's
  credential capability plus revocation", so `internal/crossdomain/authn`
  gained a second ADR 0017 contract: `CredentialWriter`
  (`WriteSystemUserCredential` over a flat `model.SystemUserCredential`
  snapshot; the plaintext crosses the seam, hashing policy stays with the
  provider). This is a deliberate ruling on the tension between the ticket's
  prose ("only through the crossdomain revocation contract and the
  Principal context") and its checklist item 1: the column move required by
  item 1 (and the password-change flow ticket 05 describes) is impossible
  without a credential capability at the same crossdomain seam, so the seam
  was extended rather than leaving iam writing the column. The iam
  `UserUsecase` stores create/update passwords through it inside the same
  transaction; its `UserRepo` neither reads nor writes `password` anymore
  (`toBiz` drops the field, create/update stop setting it), and
  `User.Password` is documented as request-input only. Login's unknown-user
  path keeps identical client behavior (decoy bcrypt + counter) via the two
  new internal sentinels `ErrLoginAccountNotFound`/`ErrLoginAccountAmbiguous`,
  which login maps onto `ErrInvalidCredentials`.
- **Application** — `internal/application/authn` owns `LoginUsecase`
  (`LoginApplication` covers the authn span of the still-unified proto
  `AuthService`) and `LoginSessionUsecase` (`OnlineSessionApplication`),
  with its own `Transaction` port and test fakes. iam's remaining span of
  `AuthUsecase`/`AuthApplication` became
  `RoleAssignmentUsecase`/`RoleAssignmentApplication` (its ticket-06 name);
  `server` joins the two spans into the one `AuthServiceServer` registration
  with an embedding composite until the proto split. Login's Authorization
  Snapshot still resolves from the iam-side tables behind
  `LoginAccountStore.LoadSnapshot` — the documented interim that ticket 04
  flips.
- **Infra** — `internal/infra/authn` (package `authninfra`) holds the sa-token
  machinery (manager, Redis storage, failure classification), the login
  backend (now answering `FindLoginAccount` from the user table's
  credential column), the login-session repo, the counters, the
  authenticator, and the new `CredentialRepo` adapter. The cross-module
  integration family (security-change, cross-instance, session-expiry,
  auth-failure, database-tx plus its shared harness) moved with it so the
  unexported key-prefix helpers stay in one package; the harness imports
  `iaminfra`/`iamapp` constructors for the iam side.
- **Pins** — `internal/layering_contract_test.go` gained
  `TestDomainPackagesAreIslands` (no domain package imports another) and
  `TestModuleSplitBoundaries` (application/authn imports nothing iam;
  application/iam imports no authn application/infra). Wire regenerated;
  `make all` and the full test suite are green on the branch. AGENTS.md's
  Principal pointer now reads `domain/authn.Principal`.

**One deliberate behavior delta (found in code review).** On main,
`UpdateUser` classified revocation with the *persisted* record as receiver,
and the repo's `toBiz` mapped the stored hash into `Password` — non-empty for
every real user — so the domain policy's `u.Password != ""` password branch
matched on **every** update: even a nickname-only edit revoked all of the
user's Login Sessions in production. The unit-test fake returned the request
DO (empty password), so the tests encoded the *intended* behavior
("other profile writes do not affect authorization", per the domain policy
doc) while production did the opposite — a latent bug. With the credential
column gone from iam's reads, the persisted receiver no longer carries a
password at all, so the classification now runs on the requested change
(`u.SessionRevocationFrom(result.Previous, …)`): profile-only updates skip
revocation; password and status changes revoke exactly as before. The delta
revokes strictly less, aligns production with the documented domain policy
and the existing unit tests, and preserving main's literal behavior is
impossible without iam re-reading the credential column (checklist item 1).
Known interims accepted by this ticket: Login's Authorization Snapshot still
resolves from iam-side tables inside `LoadSnapshot` (flips in ticket 04);
`infra/iam/menu.go` maps route-loading failures onto
`authndomain.ErrAuthBackendUnavailable` (moves with GetRouters in ticket 04);
the integration-test harness in `infra/authn` imports both modules'
constructors (test-only, unchanged shape from main).
