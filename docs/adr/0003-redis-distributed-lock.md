# Distributed lock: redislock primitives behind a WithLock closure

Business code needs cross-instance mutual exclusion — concurrent-write guards
on shared resources, duplicate-submit protection, single-runner jobs. The
stack already runs single-instance Redis through the shared `*redis.Client`
in `internal/infra/store`, and the vendored RuoYi reference uses Redisson,
whose watchdog-style auto-renewal is the ergonomics its callers expect.
Decided 2026-08-27:

- `github.com/bsm/redislock` provides the primitives (random token, SET NX
  PX, Lua compare-and-delete release, retry strategies); an in-repo wrapper
  provides the ergonomics. The business-facing API is one closure,
  `WithLock(ctx, key, fn, opts...)`, symmetric with `Transaction.InTx`.
- One shared contract package `internal/lock` (interface, sentinel errors,
  functional options). Application modules import the contract; the Redis
  implementation lives in `internal/infra/locker` on `*store.Data`, bound
  once via the fx container. Unlike `Transaction` the lock has no module-specific
  narrowing, so per-module interface copies buy nothing.
- Keys: the wrapper prefixes `glex:lock:`; call sites pass
  `module:resource[:id]` (`access:menu-sync`, `user:42`).
- Acquisition is fail-fast by default, returning `ErrLockBusy` (kratos
  Conflict `LOCK_BUSY`, HTTP 409); `WithWait(d)` opts into bounded waiting.
  Duplicate submits therefore surface as 409 — true idempotency (returning
  the first result) is a separate idempotency-key concern, out of scope.
- Renewal runs by default: a watchdog refreshes every TTL/3 while `fn`
  runs; `WithFixedTTL` opts out. Defaults are hardcoded (TTL 30s),
  overridable per call with `WithTTL`; no config section until multi-env
  needs appear.
- Honest mutual exclusion: a definitive renewal failure (`ErrLockNotHeld`)
  cancels `fn`'s context and returns `ErrLockLost` (HTTP 500 `LOCK_LOST`);
  transient renewal errors retry inside the watchdog loop. A lost lock
  stops the critical section rather than letting it run unlocked; callers
  retry the whole operation.

## Considered Options

- **Chosen:** bsm/redislock primitives + an in-repo wrapper. The closure
  API, watchdog, and error mapping are ours either way; the library carries
  the token/release-script/retry correctness that is easy to get subtly
  wrong.
- **Rejected:** go-redsync/redsync v4 — implements Redlock majority across
  multiple independent Redis nodes; we run one instance, and Redis itself
  hedges on recommending Redlock.
- **Rejected:** hand-rolled SetNX + Lua on go-redis — the login-counter
  idiom proves it feasible in ~200 lines, but it re-treads battle-tested
  upstream code to avoid one small dependency.
- **Rejected:** etcd `concurrency` — strongest consistency and built-in
  session renewal, but new infrastructure; the stack is Redis + PostgreSQL.
- **Rejected:** PostgreSQL advisory locks — session-scoped locks pin a
  pooled connection; xact-scoped locks only span one transaction and cannot
  cover a job-length critical section; ent does not surface them.

## Consequences

- A wedged critical section holds its lock until its own context deadline —
  bounded timeouts remain business code's job (the `store.BoundedTimeout`
  idiom).
- Swapping the implementation later (e.g. redsync under a multi-node Redis)
  hides behind `internal/lock`; call sites are unchanged.
- Tests run on miniredis (already a dev dependency), including TTL expiry
  via `FastForward`.
