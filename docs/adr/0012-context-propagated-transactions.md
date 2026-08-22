# Propagate transactions through context with a generated ent wrapper

Multi-write operations are orchestrated in Application through a
`Transaction.InTx` seam (declared in `internal/domain/iam/tx.go`), after the pattern of
[go-kratos/examples transaction/ent]. The implementation is a `Database`
wrapper generated into the ent package by a custom codegen template
(`internal/infra/ent/template/database.tmpl`, wired via `--template` in
`internal/infra/ent/generate.go`): `InTx` stores the transaction in ctx via
ent's `NewTxContext`, and every entity accessor (`db.User(ctx)`,
`db.Role(ctx)`, `db.Menu(ctx)`) resolves the client from ctx, so **nested InTx
calls join the outermost transaction** (join, not savepoint — an inner error
rolls the whole thing back) and repos take no transaction parameters.

The former Infrastructure `securityChangeCoordinator` is removed. Security-changing
repo methods (user/role/menu/user-role writes) now take their advisory locks
and mutate internally, self-wrapping in `InTx` (join when the caller already
opened one, begin when standalone), and **return the affected System User
identities** (`*UserUpdate`/`*RoleUpdate`/`*MenuUpdate` or `[]int64`) instead
of receiving a revocation policy parameter. Application operations pair those identities
with the unchanged policy classifiers (`internal/domain/iam/security_change.go`) and perform
`LoginSessionRepo.RevokeSystemUsers` as the **last step inside the same InTx
closure**, preserving ADR-0010's ordering: a Redis revocation failure rolls
PostgreSQL back; a commit failure after revocation is the accepted fail-safe
one-way inconsistency. Ordinary, non-security Redis writes happen after
commit.

Login is decomposed from the single `AuthLoginRepo.Issue` call into five
primitives — `LockAndRevalidate`, `LoadSnapshot`, `RecordLogin`,
`IssueCredential`, `DiscardCredential` — with the orchestration in
`application/iam.AuthUsecase.Login`. The database primitives run inside one InTx in that order
(lock held across all of them, per ADR-0010's shared-lock rule), and the Redis
primitive is deliberately last: a Redis failure now rolls the PostgreSQL
last-login writes back (previously compensated by hand), and a commit failure
after Redis is compensated by `DiscardCredential`. Transaction-machinery
failures (begin/commit) stay inside the 503 boundary.

The global advisory-lock order (Menu → Role → System User, ascending, with
the grant/assignment seams first in their phases) is unchanged and lives
inside `internal/infra/iam/authorization_lock.go` as free functions;
locks resolve their client from ctx, so they always run on the caller's
transaction and release at the outermost commit. Domain and Application never see them.

Supersedes the mechanism of 0010; its ordering semantics carry over unchanged.

[go-kratos/examples transaction/ent]: https://github.com/go-kratos/examples/tree/main/transaction
