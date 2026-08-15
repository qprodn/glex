# Verify cross-instance, concurrency, and failure behavior

Type: task
Status: ready-for-agent
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
