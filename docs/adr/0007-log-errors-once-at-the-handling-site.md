# Log errors once, at the handling site

Printing a log at every `err != nil` would triple- and quadruple-log a single
failure as it climbs repo → domain service → application → transport: ~79%
of the `err != nil` blocks in this repo are plain `return err`, and the
transport `logging.Server` middleware already records every error a handler
returns, with code, reason, and stack. Decided 2026-09-01: **an error is
logged exactly once, at the site that handles it** — the site that maps,
replaces, swallows, or degrades it and does not return it further.
Propagating (`return nil, err`) is not handling and logs nothing; the
transport middleware remains the safety net for whatever still reaches the
boundary.

Infra boundaries (ent/Redis adapters) follow three tiers. An unexpected raw
error — driver down, tx failure, serialization — is **log-and-map**: log it
once at `Error` and return the module's domain sentinel
(`ErrAuthBackendUnavailable`, `ErrRouteAccessUnavailable`, …); no raw
ent/pgx/redis error crosses an infra boundary. An expected business condition
maps silently (`ent.IsNotFound` → `ErrUserNotFound`). Benign errors are
exempt from logging entirely: `context.Canceled` (client disconnects),
sentinel-mapped not-found rows, and `redis.Nil` (the `observe()` hook in
`internal/infra/authn/auth_storage.go` already stays silent on it).

Levels follow severity, not occurrence: system failures log `Error`;
expected business rejections map silently to sentinels or log `Warn` at
most — never `Error`, so alerting stays meaningful. Every error log carries
the structured fields `operation` — a dotted path such as
`authn.login_by_password`, self-sufficient without the component logger —
and `error`, the raw error via `slog.Any("error", err)` so the unwrap chain
survives; printf-formatting an error into the message text is forbidden. No
hand-printed stack traces: `AddSource` and the transport middleware cover
position and stack. Explicitly ignored errors (`_ = err`) need either a
comment justifying the silence or a `Warn` when the failure is worth knowing
about (e.g. best-effort logout cleanup in `auth_login.go`). Enforcement is
review-based only; no lint gate was added.

## Considered Options

- **Chosen:** log-once at the handling site, with log-and-map mandatory at
  infra exits.
- **Rejected:** logging at every `err != nil` (the original proposal) — one
  DB failure would produce 3–4 identical lines and bury the cause in noise;
  the transport middleware already guarantees the boundary record.
- **Rejected:** keeping raw error propagation with middleware-only logging —
  raw driver text leaks toward the transport boundary; mapping at the source
  keeps client-visible errors clean and shrinks whatever the safety net has
  to catch.

## Consequences

- A single failure yields two lines with distinct roles: the infra `Error`
  carrying the raw cause, and the transport request log carrying the
  outgoing sentinel. This is the accepted shape, not double logging — the
  middleware log is a request record, not error handling.
- Existing log-and-map helpers (`auth_login.go`, `auth_storage.go`
  `observe()`, `access/menu.go` `routeAccessError`) move Warn → `Error` and
  adopt dotted `operation` names; remaining silent infra exits gain
  mapping. Implementation is tracked under `.scratch/error-logging/`.
- Panic responses are not unified, and that gap is the one thing this
  decision does not close. Source-side mapping covers every error a handler
  returns; a panic still reaches Kratos's stock recovery handler and leaves
  the client with the framework's `UNKNOWN / "unknown request error"` body
  instead of a canonical error. Normalizing it is a future work item, noted
  here rather than tracked in a document of its own.
- The former ADR 0004 (normalize untyped errors at the transport boundary)
  is retired into this decision: its source-side mapping *is* the rule
  above, its client-visible-cleanliness goal is already met, and its only
  uncovered item is the panic case. The number 0004 is left unused rather
  than reassigned.
- Amended 2026-09-03 by ADR 0011: domain and application propagation sites
  no longer stay silent — they log a `Warn` breadcrumb and return; `Error`
  remains reserved exactly as decided here.
