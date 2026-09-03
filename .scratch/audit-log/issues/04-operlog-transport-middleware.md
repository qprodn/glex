# 04 — operlog transport middleware

Status: claimed

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
