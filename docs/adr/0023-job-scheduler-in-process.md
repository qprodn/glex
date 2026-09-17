# In-process job scheduler: named Go executors, symmetric replicas, Redis fire claims

glex needs recurring work that nothing else converges: Payments sit PENDING past
their expiry until someone happens to read them (lazy settlement on the Get /
Query paths), and every future hygiene task (log sweeps, reconciliation once
its business trigger arrives) has the same shape. The back office also carries
RuoYi's 定时任务 expectation: operators manage schedules and read run history
from a console. We decided to build an **eighth module, job, designed natively
rather than ported** — the vendored RuoYi-Vue-Plus 5.x dropped its Quartz
sys_job for the external SnailJob server, so the classic reference (dynamic
invoke_target strings, misfire policies) is not even in the tree, and string
reflection onto Go methods is foreign to this codebase. Decided 2026-09-17,
before any code was written.

- **Executors are code, jobs are rows.** A Job Executor is a Go type
  implementing `Name() string` + `Run(ctx, args) (string, error)`, registered
  by its owning module into a `job.Registry` through the same fx value-group
  mechanism eventbus subscriptions use (`group:"job-executors"`, ADR 0022's
  wiring pattern). A job row carries only the executor's registered name,
  validated against the live registry at save time — a typo cannot arm a job
  that can never fire, and no operator input is ever executed. The registry
  lives at the `internal/domain/job` package root (like `internal/lock` and
  the eventbus contracts) so infra adapters such as pay's sweep executor
  implement it without importing the domain service package. A duplicate name
  at registration panics: it is a wiring bug, the same stance eventbus takes.
- **Cron is 5-field, parsed by robfig/cron/v3.** Only the parser is a
  dependency — the loop, the locks, and the persistence are ours. Expressions
  are evaluated in the **one timezone from `conf.Job.timezone`** (default
  Asia/Shanghai), never the process's local zone: replicas must agree, and a
  scheduled 02:00 must not drift with a container's TZ env. Occurrence times
  derive from the cron table, not from "now", so moderate clock skew delays a
  firing but never duplicates or drops its identity.
- **Replicas are symmetric; there is no leader election.** Every replica runs
  the tick loop (default every 10s), loads the enabled set, and computes due
  occurrences independently. Two Redis locks (ADR 0003) replace a leader:
  - `job:fire:<id>:<epoch>` claims **one occurrence** (epoch = the scheduled
    time, unix seconds) fail-fast; busy means another replica already handled
    it. This layer is not optional: with only a per-job run lock, a replica
    that finishes quickly releases it, and a later-ticking replica — which
    never recorded the occurrence — re-runs the same fire.
  - `job:run:<id>` is held for the run's duration and gives
    **no-overlap-per-job**: a still-running previous fire makes the scheduler
    skip and a manual run-now return 409 (`JOB_BUSY`, the LOCK_BUSY shape).
    Manual runs flow through the same lock, so they dedup against scheduled
    ones for free. Nesting is fixed (fire outer, run inner) — no same-key
    reentrancy, no cycle.
- **Firing is best-effort with honest gaps.** A restart advances to the next
  future occurrence — **missed fires are never backfilled** — and a crash
  mid-run abandons that occurrence. Executors must tolerate a duplicate
  firing (lock-loss cancels the run mid-flight; ADR 0003 renewal makes this
  rare) and a missed one, the same discipline eventbus handlers already carry
  (ADR 0022). Redis being down pauses firing rather than duplicating it:
  claim failures skip with a Warn; glex's login sessions are already
  Redis-hard-dependent, so the scheduler inherits the system's availability
  posture instead of inventing a degraded mode. Rolling deploys stay safe:
  an old replica lacking a new executor wins the claim and records an
  "unknown handler" failure log — drift made visible, self-healing after
  rollout.
- **Runs are bookkept, not audited.** Each completion writes one `job_log`
  row — trigger (scheduled / manual / external-reserved), status, summary or
  error (truncated), started_at, duration. There is no "running" status and
  no update path: the row appears only at completion, queried and withdrawn
  like the audit streams' write-once spirit (ADR 0012) but without their
  transport provenance — `create_by` is 0 because even a manual fire is
  system-executed. Per-run timeout (1–3600s, default from config and stamped
  into the row at create) bounds the context; panics are recovered into
  failure rows.
- **The external replacement path is a stated seam, not an accident.** The
  durable assets are the executor registry and the Fire bookkeeping:
  `Name()/Run` is exactly how SnailJob's `@JobExecutor(name=…)` and XXL-Job's
  named handlers bind, so an external scheduler adopts every executor
  unchanged. The replaceable part is one file — the tick-loop runtime —
  swapped for a client callback or an authenticated trigger endpoint
  (RunJob semantics with `trigger=external`). `conf.Job.enabled` is the
  kill switch: the day an external scheduler takes over, flip it off and no
  glex code changes. What retires then is only the CRUD surface and the
  claims — the templated investment.

The anchor use case: pay exposes `SweepExpired` and an executor named
`pay.expire-sweep`, seeded as a job row firing every minute. It closes or
settles PENDING payments past `expire_time` through the existing settle path
(already-paid race included); the lazy settlement paths remain untouched —
the sweep is additive convergence, and a payment landing PAID through it
still publishes no PaymentCompleted (the existing documented contract;
absence of the event is not absence of payment).

Considered and rejected: Quartz-style invoke_target strings (the ported
reference no longer exists in-tree and reflection on operator input is
foreign here); SnailJob or XXL-Job today (a second server, console, and token
surface for a single-service back office with no multi-language executors or
workflows — revisit through the replacement seam when a real trigger
arrives); a DB-CAS `next_fire_at` claim (Quartz's row-lock lineage, which
RuoYi 5.x itself moved away from — and ADR 0003 already standardized Redis
for mutual exclusion); leader election (a lease, renewal, and split-brain
reasoning to avoid what two fail-fast SetNX calls already guarantee);
backfilling missed fires (a catch-up storm after downtime buys nothing the
lazy paths don't already provide for the anchor use case).

## Consequences

- Executors are idempotent-tolerant by convention: duplicate and missed
  firings are normal operation, and money-critical convergence (the pay
  sweep) settles through CAS domain paths, never through the job's own state.
- Firing latency is bounded by the tick interval, not the cron granularity —
  a 10s tick fires a `* * * * *` job within ~10s of the minute.
- Operator UX matches RuoYi's 定时任务 pages (list, cron form, pause/resume,
  run-now, log history) minus misfire/concurrency policies, which the
  skip-and-no-overlap semantics replace outright.
- Job rows are operational config: their withdrawal keeps history readable
  (logs carry a handler snapshot) but stops future firing immediately, since
  the tick scan sees only active rows.
