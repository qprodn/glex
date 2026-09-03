# Audit logs recorded best-effort at their own boundary

RuoYi pairs `sys_oper_log` with `sys_logininfor`: one records what
authenticated operators did, the other records sign-ins and session ends.
glex ports both as the audit module — `sys_oper_log` and `sys_login_log` (the
RuoYi table name concatenated past readability; the file module's
`sys_oss` → `sys_file` rename is the precedent) — and, decided 2026-09-03,
pins three properties: **each stream is recorded at the boundary that owns
the fact, the write is best-effort, and history is withdrawn, never
truncated**.

**Capture at the owning boundary.** Oper Log rows are written by a transport
middleware (`pkg/middleware/operlog`): the kratos operation name, principal,
redacted request, outcome, and latency are all in scope exactly there, and
the hand-written transfer routes run the same chain via `ctx.Middleware`, so
one hook covers every mutating operation without annotating call sites
(RuoYi's `@Log` annotation is per-controller opt-in; a middleware is
opt-out). Login Log rows are written by authn — only it knows whether a
login attempt succeeded, why it failed, and who forced a session to end —
reaching the audit module through the `crossdomain/audit.LoginLogWriter`
contract like any other cross-module capability (ADR 0005). A transport
middleware cannot import `internal/`, so it declares its own `Recorder` port
and the composition root adapts the audit domain service into it, the way
the auth middleware receives its `Authenticator`.

**Best-effort.** A failed audit write logs `Warn` at its handling site
(ADR 0007) and is dropped; it never fails the operation being recorded.
The insert runs on a context detached from the request (`WithoutCancel`) so
a disconnected client cannot cancel its own audit trail, with a short
timeout so the detachment cannot leak. Audit trails are evidence, not a
consistency requirement — an operation must not roll back because its
shadow failed.

**Withdrawal, not truncation.** RuoYi's 清空 empties the log tables; ADR 0002
forbids hard deletes. Log rows are ordinary soft-delete rows: the API offers
singular delete (withdraw) and nothing else — rows are write-once, no
update, no bulk clear; the page batch-deletes a selection.
