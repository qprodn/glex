# 04 — operlog transport middleware

Status: resolved

`pkg/middleware/operlog`: a kratos middleware that records the request after
the handler returns. Own port `Recorder` (pkg never imports `internal/`);
the composition root adapts the audit domain service into it, mirroring the
`authmw` authenticatorAdapter. Skips: explicit operation set (authn
Login/Logout — Login Log events) and safe methods (GET/HEAD/OPTIONS).
Identity comes from the auth middleware's context value via a narrow
interface assertion (no `internal/` import). Request payloads are
protojson-marshaled with secret keys scrubbed; marshal failures record a
placeholder, never block. The insert runs on a `context.WithoutCancel`
context with a short timeout; a failed record logs `Warn` (handling site,
ADR 0007/0012) and never fails the business response. Wired after
`validate.Middleware()` in both HTTP and gRPC chains (principal must be
present).

## Accept

- Middleware tests: skips safe methods + skipped operations, records
  success and failure outcomes, redacts secrets, swallows recorder errors.

## Comments

- 2026-09-04: Implemented: pkg/middleware/operlog with its own Recorder/Identity ports (pkg never imports internal/), WithSkip, safe-method skip, protojson payload marshal with secret-key scrub (password/old_password/new_password/confirm_password/access_token/refresh_token), detached + 3s-timeout record context, Warn-and-drop on recorder failure. Wired after validate.Middleware() on both HTTP and gRPC; HTTP skips authn Login/Logout. Tests pin recording, skips, failure outcome, swallow, and the scrub pass.
