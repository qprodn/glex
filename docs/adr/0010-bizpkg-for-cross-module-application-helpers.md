# bizpkg for cross-module application helpers

Four application modules (access, dict, file, user) each carried a
byte-identical unexported `auditActor` — 19 call sites reading the
authenticated Principal's System User id out of the request context, `0` when
the system itself acts (ADR 0002). The duplication sat on a rule that decides
audit attribution, so silent drift in one copy meant audit fields quietly
written wrong. Decided 2026-09-02: **deduplicate into `internal/bizpkg`, a
flat package for cross-module application helpers**, first tenant
`bizpkg.AuditActor`, and delete the four copies.

Placement was weighed against two alternatives. The authn domain service
(next to `PrincipalFromContext`) was rejected because the audit-stamping
convention is an application-layer rule, not authn's; `pkg/` was rejected
because pkg stays domain-agnostic transport tooling that never imports
`internal`, and `AuditActor` would have dragged the System-User-id concept
into it. bizpkg may import domain services and `pkg/`; only application
services import bizpkg.

Admission rule (what keeps bizpkg from becoming a junk drawer or a second
crossdomain): helpers must be **stateless, derived only from the request
context, and never a capability call** — module-to-module capabilities stay
behind crossdomain contracts with constructor injection (ADR 0005); there is
deliberately no service locator here. Principal extraction keeps one source
of truth: bizpkg delegates to `authnservice.PrincipalFromContext` rather than
re-reading the middleware's context key.
