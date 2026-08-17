# Verify cross-instance, concurrency, and failure behavior

Type: task
Status: done 2026-08-17
Blocked by: 06, 07, 08, 09

## Objective

Prove the high-risk behavior against real PostgreSQL and Redis rather than
shipping based only on fake repositories.

## Work

- Build repeatable integration fixtures for real PostgreSQL and Redis with no
  embedded credentials.
- Run two independent application/provider graphs against the same stores.
- Cover concurrent distinct login, same-device metadata, shared snapshot
  rebuild, cross-instance logout/kickout, and five-Session eviction.
- Cover fixed expiry, idle expiry, and active-timeout refresh/check behavior.
- Cover every authorization invalidation trigger and self-password exception.
- Inject Redis read/write/timeout failures and PostgreSQL commit failure at the
  required protocol points.
- Verify safe one-way inconsistency: unnecessary logout is possible, stale
  committed authorization is not.
- Exercise malformed Session JSON/storage errors and prove they produce 503,
  not 401/403.
- Run the full suite repeatedly under `go test -race`.

## Acceptance

- Every verification item in `spec.md` has an automated test or an explicit
  documented reason it cannot be automated.
- Tests reproduce the known v0.2.4 account/device mapping hazard and prove the
  project index prevents it.
- No raw bearer token appears in normal logs or failure output.
- `make all`, `go test ./...`, and `go test -race ./...` pass.

## Result

### Fixtures

`internal/data/integration_stores_test.go` owns the harness: real stores are
gated on `GLEX_TEST_POSTGRES_DSN` and `GLEX_TEST_REDIS_ADDR` (no credentials
in the repo), every test gets a unique glex auth namespace that is unlinked on
cleanup, and `newGraph` composes each instance from the same constructors
`data.ProviderSet` wires in production (`Data` and the storage adapter are
built directly so each graph can point its own clients at the shared stores).
Two graphs over one store pair model two deployed instances; all shared state
must live in PostgreSQL and Redis.

### Defects the real-store run exposed (all fixed here)

1. No PostgreSQL connection could be opened at all: pgx registers the
   `database/sql` driver as `pgx` while the generated ent client only accepts
   the `postgres` dialect. `data.go` now registers pgx's driver under
   `postgres`.
2. The authorization advisory locks never executed against real PostgreSQL.
   A raw `entsql.Expr` placeholder `?` is invalid PG syntax, and binding the
   key through `Builder.Arg` gets silently renumbered onto the statement's
   earlier WHERE parameter — so every lock hashed the bare entity id,
   collapsing the per-kind lock namespaces (menu/role/user/grant). Locks are
   now taken on a Go-computed FNV-1a hash of the full namespaced key,
   embedded as an integer literal.
3. A System User with no Roles could never authenticate: the snapshot writer
   serialized empty role/permission sets as JSON `null` (nil slice), which the
   snapshot decoder rejects, turning every request into a spurious 503. Empty
   sets now round-trip as `[]`.

### Spec verification coverage

Unit coverage (`spec.md` "Required unit coverage"):

| Spec item | Test |
|---|---|
| strict Bearer parser; cookie/query/bare rejection | `internal/server/auth_middleware_test.go` `TestStrictBearerCredentialParser` |
| policy validation + startup completeness | `internal/server/auth_policy_test.go`; `api/system/auth/v1/policy_test.go` |
| Principal context access | `internal/biz/principal_test.go` |
| password/account-state behavior | `internal/biz/login_test.go`, `internal/biz/auth_test.go` |
| dual login counters | `internal/data/login_counter_test.go` |
| snapshot + superadmin wildcard | `internal/data/auth_login_test.go` `TestAuthorizationSnapshotAddsWildcardOnlyThroughSuperadminRole` |
| 401/403/503 mapping | `internal/server/auth_middleware_test.go`, `internal/data/auth_manager_test.go` `TestClassifyAuthFailure` |
| Logout optional-auth semantics | `internal/server/auth_routes_test.go` `TestRuoYiAuthAndOnlineSessionRoutesAndPolicies` |
| Session ownership + safe Session IDs | `internal/data/login_session_test.go` `TestOwnershipCheckedLogoutCannotRevokeAnotherSystemUsersSession`; replay check in routes test |
| router filtering/tree construction | `internal/data/menu_routes_test.go`, `internal/service/current_user_routes_test.go` |

Real-store integration coverage (`spec.md` "Required real PostgreSQL/Redis
integration coverage"):

| Spec item | Test |
|---|---|
| two service instances sharing Session state | `cross_instance_integration_test.go` (two-graph fixture, all tests) |
| distinct concurrent tokens + shared snapshot | `TestTwoInstancesIssueDistinctConcurrentLoginsIntegration`, `TestSecondInstanceLoginRebuildsSharedAuthorizationSnapshotIntegration` |
| five-Session oldest eviction | `TestSixthLoginAcrossInstancesEvictsOldestSessionIntegration` (cross-instance, same device) |
| fixed and idle expiry | `session_expiry_integration_test.go` (real Redis TTLs, real clocks) |
| last-Session Account Session cleanup | `TestCrossInstanceLogoutAndKickoutRevokesEverywhereIntegration` |
| login rebuilds Account Session after Manager.Login clears it | `TestSecondInstanceLoginRebuildsSharedAuthorizationSnapshotIntegration` |
| every Role/Menu/User/password revocation trigger | `security_change_integration_test.go` `TestSecurityChangeTriggersRevokeAffectedSessionsIntegration` |
| self-password change preserves only current Session | `TestSelfPasswordChangePreservesOnlyCurrentSessionIntegration` |
| Redis failure rolls back PostgreSQL | `TestSecurityChangeRedisFailureRollsBackIntegration` |
| Redis success + PostgreSQL commit failure → unnecessary logout only | `TestSecurityChangeCommitFailureLeavesPostgresUnchangedAndRedisRevokedIntegration` |
| concurrent login/assignment/grant/menu-change races | `TestConcurrentLoginAndEachSecurityRelationshipChangeLeaveNoStaleSessionIntegration` |
| Redis partial-operation behavior / Lua atomicity | `TestRevokeBatchValidatesAllBeforeWritingAgainstRealRedisIntegration` (real EVAL); unit twin `TestRevokeSystemUsersIsOneAtomicValidatedBatch` |

Additional ticket items:

| Item | Test |
|---|---|
| sa-token account/device mapping hazard + index prevention | `TestSaTokenDeviceMappingHazardIsContainedByProjectIndex` (always runs) |
| Redis read/write failure at auth/list/logout/kickout points | `TestRedisReadWriteFailureReturnsServiceUnavailableIntegration` |
| Redis timeout bounded and 503 | `TestRedisTimeoutFailsBoundedAndUnavailable` |
| login write failure registers nothing, rolls back last-login | `TestLoginWriteFailureRollsBackAndRegistersNothingIntegration` |
| malformed Session JSON → 503 not 401/403 | `TestMalformedLoginSessionMetadataReturnsServiceUnavailableIntegration` |
| corrupt Account Session fails closed (no empty-permission Principal) | `TestCorruptAccountSessionFailsClosedIntegration` |
| no raw bearer token in logs | `TestIntegrationFlowsNeverLogBearerTokensIntegration`; `auth_storage_test.go` `TestAuthStorageNeverLogsBearerToken` |

### Not automated, with reasons

- The transient window in which a concurrent request observes an empty
  snapshot between `Manager.Login`'s wipe and the wrapper's `SetMulti` restore
  cannot be observed deterministically from outside the locked flow. The
  enforced invariant is tested instead: no credential is returned before the
  snapshot exists, and every live token sees the restored snapshot
  (`TestSecondInstanceLoginRebuildsSharedAuthorizationSnapshotIntegration`
  authenticates both instances' tokens immediately after each login).
- The exclusive edge-proxy network topology (port unreachable, inbound
  forwarding header stripped) is a deployment property, not observable from
  the Go test process; coverage is the startup assertion
  `TestExampleAuthConfigurationMatchesConfirmedDefaults` plus config
  validation rejecting an unconfirmed topology.
- The spec names sa-token v0.2.4; that version does not exist upstream. The
  pin (and these tests) target v0.2.3, the latest published release, whose
  `Manager.Login` and device-mapping behavior the spec's v0.2.4 notes
  describe.
