## Database conventions

Every PostgreSQL table carries the five audit fields — `create_by`,
`update_by` (bigint, 0 = the system itself), `created_at`, `updated_at`
(timestamptz, `now()`), `deleted_at` (timestamptz, nullable) — via the shared
ent audit mixin (`internal/infra/ent/schema/audit.go`). No table hard-deletes
rows: withdrawal sets `deleted_at` (ADR 0002). Application services stamp
`create_by`/`update_by` from the authenticated Principal; client DTOs never
carry them. Redis login-session lifecycle is outside the rule. Join tables
carry a snowflake surrogate PK plus a partial unique index on the id pair
`WHERE deleted_at IS NULL`; a re-granted pair inserts a new row.

## Error handling and logging

An error is logged at `Error` exactly once, where it is handled — the site
that maps, replaces, swallows, or degrades it instead of returning it (ADR
0007). Infra adapters never let a raw ent/redis error escape — log-and-map:
unexpected failures log `Error` with the raw error and return the module's
domain sentinel; expected business conditions (`ent.IsNotFound` →
`ErrUserNotFound`) map silently. Benign errors are exempt:
`context.Canceled`, sentinel-mapped not-found rows, `redis.Nil`.

Domain and application propagation sites log-and-return: every `err != nil`
that a domain service or application service returns to its caller first
logs one `Warn` breadcrumb via `logx.Propagate` (`pkg/logx`), then returns
the error unchanged (ADR 0011). Wrap-and-return (`fmt.Errorf("…: %w", err)`)
is propagation and logs too; `context.Canceled` is exempt at every layer.
The transport boundary middleware (`pkg/middleware/logging.Server`) records
whatever reaches it, demoting kratos 4xx to `Warn`, `context.Canceled` to
`Info`, and keeping 5xx/unknown at `Error`. Every error log carries
structured fields `operation` (dotted path of the site: use cases
`authn.login`, domain steps `authn.svc.verify_password`, repo exits
`user.find_by_id`) and `error` (`slog.Any("error", err)`); never printf an
error into the message text, never hand-print a stack. An explicit `_ = err`
needs a justifying comment, or a `Warn` when the failure is worth knowing
about. `Error` is reserved for system failures — business rejections appear
only as `Warn` (breadcrumbs and boundary). Enforcement is review-based, no
lint.

## Shared application helpers

Cross-module application helpers live in `internal/bizpkg`, a flat package:
stateless, derived only from the request context, never a capability call —
module-to-module capabilities still go through crossdomain contracts (ADR
0005). bizpkg may import domain services and `pkg/`; only application
services import bizpkg (ADR 0010).

## Generation and generated files

Regenerate via `make api`, `make config`, `make all`, or the owning generator.
Never hand-edit `*.pb.go`, `*_grpc.pb.go`, `*_http.pb.go`, or Ent-generated
files.

## Commits and security

- Conventional Commits: `feat:`, `fix:`, `refactor:`, `chore(deps):`, `docs:`,
  `test:`. Regenerated files belong in the same commit as their source.
- Never commit real credentials in `configs/config.yaml`.

### Issue tracker

Issues live as markdown files under `.scratch/<feature>/`. See
`docs/agents/issue-tracker.md`.

### Triage labels

Five canonical roles map to same-named labels (`needs-triage`, `needs-info`,
`ready-for-agent`, `ready-for-human`, `wontfix`). See
`docs/agents/triage-labels.md`.

### Domain docs

Single-context: one root `CONTEXT.md` plus `docs/adr/`. See
`docs/agents/domain.md`.
