# Make security-changing writes revoke Sessions before commit

Type: task
Status: ready-for-human
Blocked by: 03, 04, 05

## Objective

Integrate the fail-safe PostgreSQL/Redis protocol into every existing mutation
that can invalidate an Authorization Snapshot.

## Work

- Design one transaction coordinator/data seam that keeps transaction and Redis
  mechanics out of service and biz while allowing affected users to be resolved
  and locked before commit.
- Document and implement one global advisory-lock order across Menu, Role, and
  System User identities. Re-read assignments/grants after locking when the
  initial affected-user lookup can race.
- Apply the protocol to Role Assignment and Menu Grant replacement.
- Apply it to Role `role_key`/status/deletion and Menu `perms`/status/deletion.
- Apply it to System User status/deletion and password writes.
- For self-service password change, exclude Principal's current Session ID and
  revoke the rest; administrative reset revokes all.
- Perform Redis revocation before PostgreSQL commit. Redis failure rolls back;
  Redis success followed by commit failure deliberately leaves unnecessary
  logout.
- Fix stale comments that claim a pre-existing global auth middleware.

## Acceptance

- Every trigger listed in `spec.md` has a focused biz test and an integration
  test proving the affected Sessions are unusable.
- Injected Redis failure leaves PostgreSQL unchanged.
- Injected commit failure after Redis success leaves PostgreSQL unchanged and
  Sessions revoked.
- Concurrent login/Role Assignment/Menu Grant/Menu-permission tests cannot
  produce a valid Session with a committed stale permission snapshot.
- `go test -race` reports no data races and lock-order tests do not deadlock.

## Comments

- Implemented one shared data-layer security-change coordinator. Its protocol
  is PostgreSQL transaction → namespaced advisory locks in Menu/Role/System
  User order (ascending IDs within each type) → lock-protected relationship
  re-read → database mutation → one atomic Redis batch revocation → PostgreSQL
  commit. Per-Role Menu Grant and per-User Role Assignment seams stabilize old
  relationship sets before concrete identity locks and the protected re-read.
  Redis failure rolls the database back; commit failure after Redis success
  intentionally leaves Sessions revoked.
- Integrated Role Assignment and Menu Grant replacement; Role key/status/delete;
  Menu perms/status/delete; and System User status/delete/password writes.
  Self password changes preserve only Principal's current Session, while an
  administrative reset revokes every Session.
- Added focused biz coverage for every trigger, deterministic
  lock/failure-injection tests, and optional real PostgreSQL/Redis integration
  coverage for every trigger, Redis rollback, preservation, and concurrent
  login/assignment/grant/menu-permission mutation. The real-backend suite is
  enabled with `GLEX_TEST_POSTGRES_DSN` and `GLEX_TEST_REDIS_ADDR`; ordinary CI
  remains credential-free. `make all`, `go test ./...`, and
  `go test -race ./...` pass.
- Two-axis review against baseline `0c7a75f15f090ac8c3d73b21bfaf36824b33fce1`
  completed with zero remaining Standards or Spec findings.
