# Propagation breadcrumbs: log-and-return at domain and application

ADR 0007 ruled propagation silent after finding ~79% of the `err != nil`
blocks were plain `return err`, and it holds for the levels: one failure
still logs `Error` only where it is handled — the infra log-and-map of the
raw cause, a genuine degrade/swallow, and the transport request record. But
the silence at the layers in between hid the failure's path: the domain
sentinels are package-level values with no stack, so a `USER_STORE_UNAVAILABLE`
reaching the boundary carried no trace of which use case and which domain
step it had climbed through. A field study of coze-studio
(`.scratch/error-logging/research-coze-studio-error-logging.md`) confirmed
the demand — it log-and-returns everywhere — and also the failure mode of
copying it: ~530 `Error` sites that re-log one DB failure three times and
dilute `Error` past alerting use. Decided 2026-09-03: **domain services and
application services keep returning the error unchanged, and every
propagation site also logs a breadcrumb at `Warn`** — log-and-return, not
log-and-handle.

Levels stay load-bearing. `Error` is unchanged from ADR 0007: reserved for
handling sites, so one failure produces a `Warn` trail (domain step →
application use case) capped by the same two `Error` anchors as before —
the infra log-and-map with the raw cause, and the boundary record. The
transport boundary stops logging every error at `Error`: kratos
`logging.Server` logs any non-nil error at `Error`, which contradicted ADR
0007's own "business rejections never `Error`" for every 4xx. It is replaced
by `pkg/middleware/logging.Server` with the same attributes and three
levels: kratos 4xx → `Warn`, `context.Canceled` → `Info` (benign, ADR 0007's
exempt list), 5xx/unknown → `Error`.

Mechanics: propagation sites call `logx.Propagate(ctx, logger, operation,
err)` (`pkg/logx` — stdlib-only, so domain and application may both import
it) and then return. `operation` is the dotted path of the propagating site:
application use cases `<module>.<rpc_snake>` (e.g. `authn.login`), domain
service steps `<module>.svc.<method>` (e.g. `authn.svc.verify_password`);
infra keeps its repo-operation names (`user.find_by_id`), so one store
failure reads as a complete trail across the four layers. Wrap-and-return
(`fmt.Errorf("…: %w", err)`) is propagation and logs too. `context.Canceled`
is exempt at every layer — a client disconnect is not a failure. Handling
sites are unchanged: a site that maps, replaces, swallows, or degrades logs
per ADR 0007 (business sentinels a site creates still log nothing there;
the boundary record owns them). Enforcement stays review-based.

## Considered Options

- **Chosen:** `Warn` breadcrumbs at domain/application propagation sites,
  plus boundary demotion of 4xx to `Warn`.
- **Rejected:** the coze-studio-exact pattern — `Error` at every
  propagation site. One failure would log `Error` three-plus times
  (infra, domain, application, boundary) and business not-founds would hit
  `Error`, the exact dilution the field study documents.
- **Rejected:** keeping ADR 0007's silence and relying on the boundary
  record — sentinels carry no stack, so the trail between the infra cause
  and the boundary line is unrecoverable from logs alone.
- **Rejected:** breadcrumbs in the application layer only — domain steps
  such as `authn.svc.verify_password` are where "which orchestration step
  failed" is decided; without them the trail's middle stays dark.

## Consequences

- A business rejection (wrong password) yields ~three `Warn` lines —
  domain step, application use case, boundary request — and no `Error`;
  filter by `operation` or `reason`.
- A system failure yields the `Warn` trail plus the two `Error` anchors.
  Total lines per failure rise from two to four; that is the accepted price
  of path visibility.
- `Warn` volume rises accordingly; `Error`-based alerting semantics are
  preserved by construction.
- Implementation is tracked under `.scratch/error-logging/issues/`.
