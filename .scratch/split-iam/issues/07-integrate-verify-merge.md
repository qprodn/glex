# 07 — Integrate, verify, and merge the split

**What to build:** Finish the split as one PR. Re-home the whole-graph
integration suites so they construct the three modules together — they are the
safety net for the cross-module seams (login → snapshot, security change →
revocation). Update the architecture/layering contract tests for the new
package prefixes and the crossdomain package, encoding ADR 0016's boundaries.
Update AGENTS.md and the layer READMEs: Principal now lives at
`domain/authn`, directory examples show the three modules, and the crossdomain
rules describe the anti-corruption layer shape — per-provider
contract/model/impl/mock packages, every cross-module call through the layer,
constructor-injected consumption, no service-locator defaults (ADR 0017). Run the full suite including the env-gated
integration tests, then merge the integration branch to main as the single
split PR — main never saw an intermediate state.

**Blocked by:** 06 — split proto contracts.

**Status:** done

- [x] Integration suites cover login → Authorization Snapshot → revocation and cross-module security changes against the new structure
- [x] Architecture/layering tests encode the ADR 0016 boundaries, including that domain imports nothing new
- [x] AGENTS.md and layer READMEs match the new structure; glossary already consistent with CONTEXT.md
- [x] `make all` and the full test suite (including env-gated integration) green on the branch; single PR merged to main

## Comments

Implemented on the `split-iam` integration branch as the split's final ticket;
the branch merges to main as the single split PR:

- **Whole-graph suites re-homed** — the six integration files moved from
  `infra/authn` to the new test-only `internal/integration` package
  (`integration_stores`, `security_change`, `cross_instance`, `database_tx`,
  `session_expiry`, `auth_failure`), joined by the login ordering proof from
  `infra/authn/auth_login_test.go` (now `login_transaction_test.go`, per
  ticket 04's scheduling). The harness composes all three modules through
  their exported constructors exactly as cmd wires them; test-only wiring is
  gone — session minting goes through the production
  `CredentialIssuer.IssueCredential` instead of the in-package
  `loginAndReplaceAuthorizationSnapshot` helper, and graphs build their
  sa-token storage via `NewAuthStorage` like the provider set does. The
  authn-infra cross-module imports the old in-package placement allowed are
  closed: `AuthManagerKeyPrefix` and `LoginSessionMetadataPrefix` are
  exported as the documented Redis key contract (the suites corrupt raw keys
  deliberately), while `testAuthConf`/`testLogger`/`syncBuffer`/
  `normalizeNamespace` became local fixtures. `TestConcurrentRegistrationAgainstRealRedis`
  stayed with its repo in `infra/authn`, gating on the same
  `GLEX_TEST_REDIS_ADDR`.
- **One latent suite bug fixed (found by actually running the family)** —
  `TestRoleAssignmentAndMenuGrantRedisFailureRollsBackIntegration` asserted
  `len(assignments) != 1`, but `ListRoleAssignments` lists every role in the
  shared database, so roles accumulated by earlier tests in the same run (the
  assertion is verbatim on main) made a full env-gated run fail. The
  assertion is now self-relative: the seeded role is present and unassigned,
  and nothing is assigned to the (globally unique) System User. Verified
  green against an already-contaminated database.
- **Boundary flip** — `infra/access/menu.go` maps route-access failures onto
  access-owned `ErrRouteAccessUnavailable`
  (`domain/access/menu.go`), the wire-identical twin (code, reason, message)
  of authn's `AUTH_BACKEND_UNAVAILABLE` — same ruling as access's
  `ErrUserNotFound`. The last infra→foreign-domain import is gone;
  `menu_routes_test.go` asserts the access error.
- **Architecture pins** — `TestModuleSplitBoundaries` now pins both layers
  for all three modules: application modules keep forbidding the other
  modules' domain/application/infra (with the documented import-granularity
  exception for `domain/authn` as the Principal carrier), and infra modules
  additionally forbid the other modules' crossdomain packages — test files
  included, since the infra tests are module-clean (negative-probed with
  planted imports). `TestLayerImportsFollowDDDDependencyDirection` gained the
  `crossdomain` row (no DTO/application/infra/server imports) and `domain`
  forbids `crossdomain`; `TestWholeGraphSuitesAreTestOnly` pins
  `internal/integration` to `_test.go` files only. "Domain imports nothing
  new" is the island test plus the domain row of the layer contract.
- **Docs** — AGENTS.md: the structure tree shows the three modules plus
  `internal/integration`, the application/domain examples name user/access/
  authn instead of iam, and the testing-seams section documents the
  whole-graph suites' home and env gates (the crossdomain ADR 0017 rules and
  the `domain/authn.Principal` pointer were already synced by tickets 02/03).
  `internal/infra/README.md` lists the three module adapters plus the
  module-isolation rule; `infra/access` and `infra/authn` gained READMEs
  mirroring `infra/user`'s. CONTEXT.md's glossary was already three-module
  consistent (verified; unchanged).
- **Verification** — `make all` (wire_gen byte-identical), `go vet`, and the
  full suite green, including the env-gated family against real
  PostgreSQL 16 / Redis 7 containers (GLEX_TEST_POSTGRES_DSN +
  GLEX_TEST_REDIS_ADDR): login → snapshot → revocation, cross-instance
  rebuild, security-change revocation + rollback, session expiry, failure
  classification, and the no-token-leak log capture all pass. Branch merged
  to main as the single split PR.

Code-review findings (standards + spec axes): zero hard violations and no
scope creep; folded in the stale "authentication-shaped" log wording in
`routeAccessError` and the missing test-only pin for `internal/integration`.
Left deliberately: the `LoginApplication` name (the authn span of the proto
service, settled in ticket 03), `RoleAssignmentUsecase`'s pre-tx existence
gate (accepted in ticket 04), and the harness's local `normalizeNamespace`
mirror (drift fails the corruption suites loudly rather than silently).
