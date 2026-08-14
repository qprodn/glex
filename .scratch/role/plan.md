# Role Module — Plan (as-built)

**Status:** implemented 2026-08-14 (builds, `go vet` clean, biz unit tests pass).
PG integration + runtime smoke still pending (same posture as user/menu).
**Part of:** glex — RuoYi-Vue-Plus → Go/Kratos rewrite, module-by-module.
System User done; Menu done; **Role** next.

> Decisions: this plan. No ADR — session decision (Q7=B); the scope cut mirrors
> ADR-0005 but is recorded here instead. Layering per `CLAUDE.md`/`AGENTS.md`.
> Conventions per ADR-0001 (single-tenant) / 0003 (Postgres) / 0004 (no auth yet).

## Goal

Back-office **role CRUD**, ported from RuoYi's `sys_role`, written fresh on
Kratos. **Basic CRUD only — no associations with other tables** (Q1): no
`sys_role_menu` / `roleMenuTreeselect`, no user↔role links, no data scope, no
`changeStatus` RPC (status is an ordinary updatable field), no import/export,
no seed data.

Cadence mirrors user/menu: write → test → review → next module.

## In scope

- `sys_role` table (PostgreSQL): intrinsic fields only (Q2), no data.
- CRUD RPCs: Create / Get / List (paginated) / Update (field-mask) / Delete
  (soft).
- **Repo-wide uniqueness convention (Q3/Q8/Q9):** uniqueness is enforced by
  code checks against *active* rows (`deleted_at is null`) — no DB unique
  constraints anywhere. Soft-deleted values are reusable (RuoYi parity). This
  migrates `sys_user` off its DB unique constraints too (see below).
- Layered tests: in-memory fake repo (biz). PG integration + acceptance
  pending (same posture as user/menu).

## Out of scope (deferred)

| Deferred | Goes to |
|---|---|
| `sys_role_menu` + `roleMenuTreeselect` | role module's later phase (CONTEXT.md corrected) |
| `sys_user_role` + `authUser/*` + assigned-role guards | auth module |
| `data_scope` semantics + `sys_role_dept` + `deptTree` | data-scope module |
| Super-admin protection (role_id=1, `superadmin` key) | auth module (no seed data here) |
| `changeStatus` RPC, export, `optionselect`, cache eviction | later / auth module |
| Role key/name charset rules (Q4=B: non-blank only) | never — column width 256 is the only bound |
| Multi-tenancy | **never** (ADR-0001) |

## Decisions (locked this round)

- **Scope = CRUD only** [Q1=A]. `sys_role_menu` deferred within the role
  module; CONTEXT.md's "the link lives in the role module" is corrected to
  "deferred to the role module's later phase".
- **Schema = intrinsic fields only** [Q2=A]. Drop `data_scope`,
  `menu_check_strictly`, `dept_check_strictly`, `create_dept`, `del_flag` —
  they belong to data-scope / role-menu / role-dept work.
- **Uniqueness = code checks, no DB constraints** [Q3=B + Q8=A + Q9=A].
  Repo-wide convention: checks query active rows only; **soft-deleted values
  are reusable** (RuoYi parity). Racy under concurrent writes (same as RuoYi).
- **`role_key` validation = non-blank only** [Q4=B]. REQUIRED catches empty
  strings via the validate middleware (`fieldbehavior` treats `len(s) == 0`
  as missing). No charset rule, no normalization; column `varchar(256)`.
- **List = AIP page-token pagination** [Q5=A], default `page_size=20`, default
  order `role_sort` asc + `role_id` asc tie-break (pagination stability),
  exact-match filters on `role_name`, `role_key`, `status`.
- **`status` included** [Q6=A]: `smallint` 0正常 1停用, ordinary updatable
  field, no dedicated RPC.
- **Docs = plan + CONTEXT.md, no ADR** [Q7=B].
- **`role_sort`** [default]: not REQUIRED (REQUIRED would reject 0), defaults
  to 0, no range check (menu `order_num` precedent; RuoYi `int(4)`).
- **`role_name`/`role_key` editable** [default]: update uniqueness check
  excludes self (RuoYi allows editing roleKey).
- **Delete** [default]: single delete, always allowed (no join guards exist),
  soft delete via `deleted_at`; `Get` after delete → NOT_FOUND.
- **Error reasons** [default]: `ROLE_NOT_FOUND`, `ROLE_NAME_CONFLICT`,
  `ROLE_KEY_CONFLICT`, `ROLE_INVALID` (blank name/key).
- **No seed data** [default] — super-admin protection is moot without it.
- **No web UI** [default] — matches user/menu; `make api` generates the TS
  client into `web/src/services` and refreshes `openapi.yaml`.

## Schema — `sys_role` (see `docs/db/schema.sql`)

String columns `varchar(256)` (uniform, per repo convention). **No unique
constraints** — code checks only (repo-wide convention).

```sql
create table if not exists sys_role (
    role_id     bigint       not null,
    role_name   varchar(256) not null,
    role_key    varchar(256) not null,               -- permission string (no charset rule)
    role_sort   integer      not null default 0,     -- display order
    status      smallint     not null default 0,     -- 0正常 1停用
    remark      varchar(256) not null default '',
    create_by   bigint       not null default 0,     -- audit; auth module fills
    update_by   bigint       not null default 0,
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now(),
    deleted_at  timestamptz,                         -- soft delete; NULL = active
    constraint sys_role_pkey primary key (role_id)
);
-- No unique constraints on role_name/role_key: enforced by code checks against
-- active rows; soft-deleted values are reusable (repo-wide convention).

comment on table sys_role is '角色信息表';
```

**Deviations from RuoYi `sys_role`:** dropped `data_scope` /
`menu_check_strictly` / `dept_check_strictly` / `create_dept` / `del_flag`;
`del_flag` → `deleted_at` (soft delete); `status` `char(1)` → `smallint`;
timestamps `timestamptz`; all strings `varchar(256)` (RuoYi varied 30/100/500);
no unique constraints (RuoYi has none either — app-level checks); PK snowflake.

## API

- **proto:** `api/system/role/v1/{role,error_reason}.proto`,
  `package system.role.v1`.
- **RPCs & transport:** `POST /system/role`, `GET /system/role` (paginated),
  `GET /system/role/{role_id}`, `PATCH /system/role/{role.role_id}`,
  `DELETE /system/role/{role_id}`.
- **`ListRolesRequest`:** `page_size` / `page_token` / `filter` / `order_by`
  (user-module shape); returns `RoleSet { repeated Role roles; string
  next_page_token }`. Filter idents: `role_name`, `role_key`, `status`.
- **error reasons:** `ROLE_NOT_FOUND`, `ROLE_NAME_CONFLICT`,
  `ROLE_KEY_CONFLICT`, `ROLE_INVALID` → `ErrRole<Cause>` in `biz`.

## Layering (planned)

- **DTO** — `api/system/role/v1/*.proto` (+ generated stubs via `make api`).
- **biz** — `role.go`: `Role` DO, `RoleRepo` interface (`FindByID`,
  `FindByRoleName`, `FindByRoleKey`, `ListRoles`, `CreateRole`, `UpdateRole`,
  `DeleteRole`), `RoleUsecase` (blank name/key → `ErrRoleInvalid`; uniqueness
  asserts on create and update excluding self). `errors.go`: 4 role errors.
  Reuses `pagination.go` (`ListOption`).
- **data** — `ent/schema/role.go` (table `sys_role`, snowflake id, smallint
  status, integer role_sort, soft delete), `role.go` (repo, PO↔DO, flat list
  with AIP filter/order + offset/limit, default limit 20). No unique
  constraints → no `translatePGErr` mapping (removed repo-wide with the user
  migration).
- **service** — `role.go`: `convertRole`, AIP filter/order/page-token parse,
  fieldmask update (load-current-then-apply).
- **wiring** — add to `biz`/`data`/`service` `ProviderSet`s; register HTTP/gRPC
  in `server/{http,grpc}.go`; `make all` regenerates stubs + ent + wire.
- **infra** — append `sys_role` to `docs/db/schema.sql`.

## sys_user migration (repo-wide convention, Q3=B/Q8=A/Q9=A)

Companion change in the same round, shipped as its own `refactor:` commit:

1. `internal/data/ent/schema/user.go` — drop `.Unique()` on `user_name`,
   `email`, `phone_number` (id keeps `Unique()` — it is the PK).
2. `docs/db/schema.sql` — drop the three `constraint ... unique` lines.
3. `internal/biz/user.go` — `UpdateUser` now runs the uniqueness check
   (previously DB-only); `assertUnique` gains an `excludeID` argument so an
   update that keeps its own values passes.
4. `internal/data/user.go` — delete `translatePGErr` and its call sites (no
   unique violations remain; snowflake PK collisions are not mapped).
5. `internal/biz/user_test.go` — add update-uniqueness tests (conflict with
   another user, self-pass, empty email not checked).

Behavior change: soft-deleted `user_name`/`email`/`phone_number` become
reusable (checks query active rows only) — fixes the trade-off recorded in
`.scratch/system-user/plan.md` ("soft-deleted user_name can't be reused").
Update-can't-clear-email/phone trade-off is **unchanged** (out of scope).

## Testing

- **biz (role)** — `internal/biz/role_test.go`, in-memory fake repo:
  create blank name/key rejected, name conflict, key conflict, create success
  (id assigned), update conflict excluding self, update self-pass, delete
  success / not-found.
- **biz (user)** — new update-uniqueness cases (see migration above).
- **data** — `NewRoleRepo` integration against local PG (**pending**, as
  user/menu).
- **acceptance** — create→get→list→update→soft-delete flow (**pending**
  runtime).

## Known trade-offs

1. **Endpoints unauthenticated** (ADR-0004) — auth middleware re-added by the
   auth module.
2. **Racy check-then-write** — no DB unique constraints; concurrent creates
   can duplicate `role_name`/`role_key` (RuoYi identical; roles are
   hand-curated, low risk).
3. **Soft-deleted values reusable** — `role_name`/`role_key` (and user
   name/email/phone after the migration) can be taken by new records. RuoYi
   parity; deliberate.
4. **Dev DB migration** — dev auto-migrate uses `WithDropIndex(true)`, so
   fresh dev DBs are clean; an existing dev DB may still hold the old
   constraint-backed indexes and need a one-time manual
   `alter table sys_user drop constraint ...` (production is hand-managed via
   `schema.sql`).
5. **`role_key`/`role_name` editable** — changing a key later can break
   permission grants; no grants exist yet (auth module).
6. **Update can't clear email/phone** (user module, pre-existing) — unchanged.

## Follow-ups (other modules)

- **role module later phase** — `sys_role_menu` + `roleMenuTreeselect`,
  DeleteMenu's role-assignment check.
- **auth module** — `sys_user_role`, login/session, permission resolution via
  `role_key`, super-admin protection, audit `create_by`/`update_by` fill.
- **data-scope module** — `data_scope` + `sys_role_dept` + `create_dept`.
