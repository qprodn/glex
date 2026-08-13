# System User Module — MVP Plan (as-built)

**Status:** implemented 2026-08-13 (builds, `go vet` clean, biz unit tests pass).
PG integration + runtime smoke still pending.
**Part of:** glex — RuoYi-Vue-Plus → Go/Kratos rewrite, module-by-module.

> Decisions: `CONTEXT.md` + `docs/adr/0001-0004`. Layering per `CLAUDE.md`.

## Goal

Back-office **system-user CRUD**, ported from RuoYi's `sys_user`, written fresh
on Kratos. Cadence: write → test → review → only then design the next module.

## In scope

- `sys_user` table (PostgreSQL): ported structure, no data.
- CRUD RPCs: Create / Get / List / Update (field-mask) / Delete (soft).
- bcrypt password set at the **biz** boundary.
- proto + HTTP/gRPC service, RuoYi-compatible REST prefix.
- Layered tests: in-memory fake repo (biz) + local PG (data, pending).

## Out of scope (deferred)

| Deferred | Goes to |
|---|---|
| Auth / login / session / token / middleware | auth module |
| Role / Dept / Post / Menu / permission | RBAC modules |
| Data scope / row-level security (+ `create_dept`) | data-scope module |
| OSS / avatar upload (`avatar` column kept, API deferred) | OSS module |
| Excel import/export | later |
| Multi-tenancy | **never** (ADR-0001) |

## Decisions

- **ADR-0001** single-tenant — no `tenant_id`.
- **ADR-0002** (superseded by 0004) — originally kept `admin`; now removed.
- **ADR-0003** PostgreSQL (template was MySQL).
- **ADR-0004** removed the template `admin` resource **and** `pkg/auth`; only the
  user module remains. All endpoints are currently **unauthenticated** until the
  auth module lands.
- Kratos-idiomatic; not a Sa-Token / MyBatis-Plus port.
- `user_type` removed (its real use — token namespacing / multi-strategy auth —
  belongs to the auth module).

## Schema — `sys_user` (as-built; see `docs/db/schema.sql`)

String columns use ent's default `varchar(256)` (kept uniform so `schema.sql`
stays aligned with the ent schema). Simple UNIQUE constraints (not partial) —
see trade-off below.

```sql
create table sys_user (
    user_id      bigint       not null,
    user_name    varchar(256) not null,
    nick_name    varchar(256) not null default '',
    email        varchar(256),                       -- nullable; unique when set
    phone_number varchar(256),                       -- nullable; unique when set
    gender       smallint     not null default 0,    -- 0男 1女 2未知
    status       smallint     not null default 0,    -- 0正常 1停用
    password     varchar(256) not null,              -- bcrypt hash
    avatar       varchar(256),
    remark       varchar(256),
    create_by    bigint       not null default 0,
    update_by    bigint       not null default 0,
    login_ip     varchar(256),
    login_date   timestamptz,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,                        -- soft delete
    constraint sys_user_pkey primary key (user_id),
    constraint sys_user_user_name_key unique (user_name),
    constraint sys_user_email_key unique (email),
    constraint sys_user_phone_number_key unique (phone_number)
);
```

**Deviations from RuoYi `sys_user`:** removed `dept_id`, `create_dept`,
`del_flag`, `user_type`; `create_time`/`update_time` → `created_at`/`updated_at`;
soft delete via `deleted_at` (not `del_flag`); `gender`/`status` `char(1)` →
`smallint`; timestamps `timestamptz`; `avatar` OSS-id → `varchar(256)` URL; real
UNIQUE indexes (RuoYi only had non-unique + racy check-then-write); PK snowflake.

## API

- **proto:** `api/system/user/v1/{user,error_reason}.proto`, `package system.user.v1`.
- **RPCs:** `CreateUser`, `GetUser`, `ListUsers` (AIP filter/order/page-token),
  `UpdateUser` (field-mask), `DeleteUser`.
- **Transport:** HTTP + gRPC. `POST /system/user`, `GET /system/user`,
  `GET /system/user/{user_id}`, `PATCH /system/user/{user_id}`,
  `DELETE /system/user/{user_id}`.

## Layering (done)

- **DTO** — `api/system/user/v1/*.proto` (+ generated stubs).
- **biz** — `user.go` (DO, `UserRepo`, `UserUsecase`), `errors.go` (5 errors),
  `password.go` (bcrypt — moved out of the deleted admin.go). Reuses
  `pagination.go` (`ListOption`).
- **data** — `ent/schema/user.go` (table `sys_user`, snowflake id, soft-delete),
  `user.go` (repo, PO↔DO, PG `23505` → conflict mapping), `snowflake.go`.
- **service** — `user.go` (convert, AIP list, fieldmask, password write-only).
- **wiring** — `ProviderSet`s, `server/{http,grpc}.go`, `wire_gen.go`.
- **infra** — PG driver/config, `docs/db/schema.sql`.

## Testing

- **biz** — `internal/biz/user_test.go`, in-memory fake repo (7 tests, passing).
- **data** — `NewUserRepo` integration tests against local PG (**pending**).
- **acceptance** — create→get→list→update→soft-delete flow (**pending** runtime).

## Known trade-offs (from implementation)

1. **Endpoints unauthenticated** — auth middleware removed (ADR-0004); fine pre-
   deploy, auth module adds it back.
2. **Soft delete + simple unique** — a soft-deleted `user_name`/`email`/`phone`
   can't be reused. Fix later: partial unique index `where deleted_at is null`.
3. **Update can't clear** email/phone (only set/change); empty password = unchanged.
4. **varchar(256) uniform** (not RuoYi's varied 30/50/11/100) to keep
   ent ↔ `schema.sql` aligned.
5. **Snowflake node hardcoded to 1** — configure before multi-instance.

## Follow-ups (other modules)

- **auth module** — build login/session/middleware from scratch (the template's
  was removed); protect `/system/user/*`; add gRPC auth middleware.
- **data-scope module** — add `create_dept` + row filtering.
- RBAC modules — add `dept_id` back (nullable) + role/post/menu.
