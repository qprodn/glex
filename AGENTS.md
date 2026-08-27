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

## Generation and generated files

Regenerate via `make api`, `make config`, `make all`, or the owning generator.
Never hand-edit `*.pb.go`, `*_grpc.pb.go`, `*_http.pb.go`, Ent-generated files,
or `wire_gen.go`.

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
