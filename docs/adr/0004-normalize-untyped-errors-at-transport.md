# Normalize untyped errors at the transport boundary

Infra adapters map only `ent.IsNotFound` to domain errors; everything else is
returned raw, so a leaked ent/pgx/tx error reached the default Kratos error
encoder as `{code: 500, reason: "", message: "<raw driver text>"}` — exposing
internals to clients and shipping an empty reason the frontend cannot branch
on. Decided 2026-08-27: **any error leaving a server that is not a
`go-kratos/kratos/v3/errors` error (checked via `errors.As` unwrapping, so
`%w`-wrapped business errors pass through with their reason intact) is
rewritten into a fixed response**: `{code: 500, reason: "INTERNAL_ERROR",
message: "系统出错，请稍后再试", metadata: {}}`.

The rewrite lives in one server middleware (`pkg/middleware/errnormalize`)
registered **outermost** — before `logging` — on both the HTTP and gRPC
servers, so the existing logging middleware still records the original error
and the normalizer itself stays silent (no double logging). Panics are
unified through `recovery.WithHandler` to return the same canonical error,
replacing the stock `UNKNOWN` / "unknown request error" body. The
`INTERNAL_ERROR` reason is framework-level: documented here and mapped in the
frontend i18n table, deliberately **not** added to any module's `ErrorReason`
proto enum.

## Considered Options

- **Chosen:** outermost normalization middleware on both transports. One
  implementation, identical wire behavior on HTTP and gRPC, and the raw error
  is preserved in logs by ordering rather than by extra logging code.
- **Rejected:** replacing only the HTTP error encoder — leaves gRPC leaking
  raw errors.
- **Rejected:** mapping known error families (e.g. `ent.IsConstraintError` →
  409, `context.DeadlineExceeded` → 504) inside the net. The safety net stays
  dumb and stable; known-family mapping belongs at the domain/infra source
  where the business meaning lives (e.g. the `AssertUnique` check-then-insert
  race).

## Consequences

- Response bodies no longer carry any detail about unexpected failures;
  debugging relies on the logged original error (request id correlates).
- Known gaps that now silently fall into the generic 500 — notably the
  uniqueness race leaking `ent.IsConstraintError` — are accepted for now and
  tracked for fix at the source.
- `metadata` stays empty; adding request-id correlation there is a future
  option, not part of this decision.
