# Error logging: log once at the handling site (ADR 0007)

Ship the error-logging convention decided on 2026-09-01 and recorded in
`docs/adr/0007-log-errors-once-at-the-handling-site.md`:

- An error is logged exactly once, at the site that handles it (maps,
  replaces, swallows, degrades). Propagation sites log nothing.
- Infra adapters are log-and-map end to end: unexpected raw ent/redis
  failures log `Error` with the raw error and return the module's domain
  sentinel; expected business conditions map silently; `context.Canceled`,
  sentinel-mapped not-found rows, and `redis.Nil` are exempt.
- Levels: system failures `Error`, business rejections `Warn` at most.
- Structured fields on every error log: `operation` (dotted path, e.g.
  `authn.login_by_password`) and `error` (`slog.Any("error", err)`).
- `_ = err` sites carry a justifying comment or a `Warn`.

## Scope

`internal/infra/**` (adapters only), plus the `_ =` audit across
`internal/` and `pkg/`. No application-layer changes: it stays
propagation-only.

## Out of scope

- `pkg/middleware/errnormalize` — dropped. The ADR that proposed it (0004) was
  retired into ADR 0007; the source-side log-and-map is the decision, and the
  only uncovered item is panic normalization.
- New lint gates — enforcement is review-based by decision.
- Generated code and tests.
