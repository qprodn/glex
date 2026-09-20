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

## Audit logs

Audit rows are write-once: no endpoint creates or updates them, and the only
change a row ever takes is withdrawal (ADR 0012). Oper Logs
(`sys_oper_log`, 操作日志) are minted by the transport middleware
(`pkg/middleware/operlog`) after every mutating operation except the skip
set; Login Logs (`sys_login_log`, 登录日志) are minted by authn at the
moment the outcome is known, through the `crossdomain/audit.LoginLogWriter`
contract. Both streams record best-effort: a failed write logs `Warn` at its
handling site and never fails the operation being recorded, and the insert
runs on a detached, timeout-bounded context. Request payloads are persisted
only after secret keys (`password`, tokens) are scrubbed.

## Shared application helpers

Cross-module application helpers live in `internal/bizpkg`, a flat package:
stateless, derived only from the request context, never a capability call —
module-to-module capabilities still go through crossdomain contracts (ADR
0005). bizpkg may import domain services and `pkg/`; only application
services import bizpkg (ADR 0010).

## Function signatures

Layer-facing exported functions — application services, domain services,
repository implementations, crossdomain contracts — take at most
`context.Context` plus one business object and return at most one value plus
`error`: `Create(ctx, req *CreateMenuRequest) (*entity.Menu, error)`,
`FindByID(ctx, id) (*entity.Menu, error)`, `ListAll(ctx)`. The object and the
returned value may be scalars and may be absent. When two business values
must travel together — in or out — they travel in one struct: parameters in
an `XxxRequest` type, a missing row as `nil` (or the module sentinel), a page
window as one page struct. Exempt: constructors (`New*`),
entity and value-object methods, variadic `opts ...Option`, and generated
code. bizpkg helpers follow the same shape by convention (ADR 0025).

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
