# Menu Module — Plan (as-built)

**Status:** implemented 2026-08-13 (builds, `go vet` clean, biz unit tests pass).
PG integration + runtime smoke still pending.
**Part of:** glex — RuoYi-Vue-Plus → Go/Kratos rewrite, module-by-module.
System User done; **Menu** next.

> Decisions: this plan + `docs/adr/0005`. Layering per `CLAUDE.md`. Conventions
> per ADR-0001 (single-tenant) / 0003 (Postgres) / 0004 (no auth yet).

## Goal

Back-office **menu CRUD + tree**, ported from RuoYi's `sys_menu`, written fresh
on Kratos. The menu module owns the menu *entity* and its tree. It deliberately
does **not** own route generation (`getRouters`) or the role↔menu link — those
are deferred to the auth and role modules (ADR-0005). Cadence mirrors the user
module: write → test → review → only then design the next module.

## In scope

- `sys_menu` table (PostgreSQL): full RuoYi structure ported, no data.
- CRUD RPCs: Create / Get / List (tree) / Update (field-mask) / Delete (soft).
- Tree built in-memory (flat repo query → biz builds the tree).
- `menu_type` enum (Directory/Menu/Button); `perms` stored but not enforced;
  routing columns (path/component/…) stored but inert until auth.
- Layered tests: in-memory fake repo (biz). PG integration + acceptance pending
  (same posture as the user module).

## Out of scope (deferred)

| Deferred | Goes to |
|---|---|
| `getRouters` + `RouterVo` (route tree by current user) | auth/permission module |
| `sys_role_menu` + `roleMenuTreeselect` | role module |
| `perms` matching / enforcement | auth module |
| `checkRouteConfigUnique` (path/routeName collision among M/C) | auth module |
| DeleteMenu role-assignment check (`checkMenuExistRole`) | role module |
| `create_dept` column + row-level data scope | data-scope module |
| Substring (`LIKE`) menu_name search | later (exact-match filter for now) |
| Full descendant cycle-detection on reparent | later hardening |
| Multi-tenancy | **never** (ADR-0001) |

## Decisions (locked this round)

- **Scope = CRUD + tree only** [Q1=A]. `getRouters` needs a current user +
  permission set (no auth yet, ADR-0004); `roleMenuTreeselect` needs roles
  (role module). Both unbuildable today; deferred rather than shipped half-working.
- **Schema = FULL** [Q2=FULL]. Port the entire `sys_menu` structure now; drop only
  `create_dept` (data-scope, per user-module precedent). Routing columns are
  intrinsic to a menu, not premature infra — storing them now means `getRouters`
  reads the same table later with no migration.
- **ListMenus = full tree, no pagination** [Q3=A]. Returns `MenuTree` (nested
  children). Menus are small + tree-shaped; page-token pagination stays for flat
  resources like user.
- **menu_type = proto enum stored smallint** [Q4=A]. Wire values are stable names
  (clients branch on the name, like `error_reason`); char(1)→smallint per ADR-0003.
- **Delete with children = reject** [Q5=A]. `MENU_HAS_CHILDREN`; caller deletes
  bottom-up. Matches RuoYi, avoids soft-delete-cascade on a self-referencing tree.
- **Tree strategy** [default]: repo returns a flat list (filter + order by
  `parent_id, order_num`); biz builds the tree in memory in one pass (roots =
  `parent_id == 0`). No recursive SQL/CTE.
- **parent_id root = 0 sentinel** [default], `bigint not null default 0`.
- **Soft delete via `deleted_at`** [default/convention]. Deviation from RuoYi
  (which hard-deletes menus) — kept for repo-wide consistency; see trade-offs.

## Secondary decisions (my default — adjust at review if you disagree)

- **`ext` = varchar(256)** (RuoYi 2000), honoring the locked "string columns =
  varchar(256)" rule. Say the word if you want the wide field.
- **Sibling-name uniqueness** = soft biz check on `(parent_id, menu_name)`, no DB
  unique constraint (RuoYi has none; PK is `menu_id` only). Parallels the user
  module's check-then-write. Error `MENU_NAME_CONFLICT`.
- **Reparent validation** = reject `parent_id == menu_id` only (`MENU_PARENT_INVALID`),
  matching RuoYi. Full descendant cycle-detection deferred (a cycle orphans nodes
  in the tree view; it does not crash the builder).
- **menu_name filter** = exact match (AIP string equality, consistent with user).
  Substring search deferred.
- **proto `children` = read-only** (populated on List, ignored on Create/Update),
  symmetric to how `password` is write-only in the user module.
- **icon default `''`** (RuoYi used `'#'`); no frontend coupling under scope A.

## Schema — `sys_menu` (planned; see `docs/db/schema.sql`)

String columns use ent's default `varchar(256)` (uniform with the user module).

```sql
create table if not exists sys_menu (
    menu_id     bigint       not null,
    menu_name   varchar(256) not null,
    parent_id   bigint       not null default 0,    -- 0 = root
    order_num   integer      not null default 0,
    path        varchar(256) not null default '',   -- route address (inert until auth)
    component   varchar(256) not null default '',   -- vue component path
    query_param varchar(256) not null default '',   -- route query (json object string)
    is_frame    smallint     not null default 0,    -- 0否 1是 (external link)
    is_cache    smallint     not null default 1,    -- 0不缓存 1缓存
    menu_type   smallint     not null,              -- 1目录 2菜单 3按钮 (MenuType enum)
    visible     smallint     not null default 0,    -- 0显示 1隐藏
    status      smallint     not null default 0,    -- 0正常 1停用
    perms       varchar(256) not null default '',   -- permission handle; enforced by auth
    icon        varchar(256) not null default '',
    active_menu varchar(256) not null default '',   -- active route for hidden menus
    ext         varchar(256) not null default '',   -- extension (RuoYi 2000; glex 256)
    create_by   bigint       not null default 0,    -- audit; auth module fills
    update_by   bigint       not null default 0,
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now(),
    deleted_at  timestamptz,                        -- soft delete; NULL = active
    constraint sys_menu_pkey primary key (menu_id)
);
-- No unique constraint on (parent_id, menu_name): enforced by a soft biz check.
comment on table sys_menu is '菜单权限表';
```

**Deviations from RuoYi `sys_menu`:** dropped `create_dept` (data-scope); added
`deleted_at` (soft delete — RuoYi hard-deletes menus); `char(1)` → `smallint`
(`is_frame`/`is_cache`/`menu_type`/`visible`/`status`); `is_frame`/`is_cache`
Y/N → 0/1 (N=0, Y=1); `menu_type` M/C/F → 1/2/3 (enum); timestamps `timestamptz`;
all strings `varchar(256)` (RuoYi varied 50/100/200/255/500/2000); PK snowflake.

## API

- **proto:** `api/system/menu/v1/{menu,error_reason}.proto`, `package system.menu.v1`.
- **RPCs & transport:** `POST /system/menu`, `GET /system/menu` (tree),
  `GET /system/menu/{menu_id}`, `PATCH /system/menu/{menu_id}`,
  `DELETE /system/menu/{menu_id}`.
- **`ListMenusRequest`:** `filter` + `order_by` only — **no** page_size/page_token.
  Filter idents: `menu_name`, `status`, `visible`, `menu_type`, `parent_id`.
- **`menu_type`** wire enum:

  ```proto
  enum MenuType {
    MENU_TYPE_UNSPECIFIED = 0;
    MENU_TYPE_DIRECTORY   = 1; // 目录
    MENU_TYPE_MENU        = 2; // 菜单
    MENU_TYPE_BUTTON      = 3; // 按钮
  }
  ```

- **error reasons:** `MENU_NOT_FOUND`, `MENU_HAS_CHILDREN`, `MENU_NAME_CONFLICT`,
  `MENU_PARENT_INVALID` → `ErrMenu<Cause>` in `biz`.

## Layering (planned)

- **DTO** — `api/system/menu/v1/*.proto` (+ generated stubs via `make api`).
  `Menu` carries a read-only `repeated Menu children`; `MenuTree { repeated Menu menus }`.
- **biz** — `menu.go`: `Menu` DO (+ `Children []*Menu`, `MenuType*` constants),
  `MenuRepo` interface (`FindByID`, `FindByParentAndName`, `HasChildren`,
  `ListMenus`, `CreateMenu`, `UpdateMenu`, `DeleteMenu`), `MenuUsecase`
  (type-required on create, sibling-name uniqueness, parent≠self on update,
  has-children guard on delete, `buildTree` helper). `errors.go`: 4 menu errors.
  Reuses `pagination.go` (`ListOption`).
- **data** — `ent/schema/menu.go` (table `sys_menu`, snowflake id, soft-delete),
  `menu.go` (repo, PO↔DO, flat list query with AIP filter/order, `HasChildren`
  count, `FindByParentAndName`). No unique DB constraint → no menu-specific
  `translatePGErr` mapping (passthrough).
- **service** — `menu.go`: `convertMenu` (recursive DO→DTO incl. children),
  AIP filter/order parse (**no** pagination), fieldmask update, type validation.
- **wiring** — add to `biz`/`data`/`service` `ProviderSet`s; register HTTP/gRPC in
  `server/{http,grpc}.go`; `make all` regenerates stubs + ent + wire.
- **infra** — append `sys_menu` to `docs/db/schema.sql`.

## `buildTree` (biz, pure, tested)

Group the flat list by `parent_id`; roots = `parent_id == 0`; each node attaches
its children from the map; sort each sibling group by `order_num`. Orphans (parent
soft-deleted / missing) are not attached — they remain in the DB and via `GetMenu`,
but drop out of the tree view (consistent with soft-delete semantics).

## Testing

- **biz** — `internal/biz/menu_test.go`, in-memory fake repo:
  create (type-required, sibling-name conflict), `buildTree` (nesting, order,
  orphan drop), update (name conflict excluding self, parent≠self), delete
  (has-children rejected, success).
- **data** — `NewMenuRepo` integration against local PG (**pending**, as user).
- **acceptance** — create→tree→update→soft-delete flow (**pending** runtime).

## Known trade-offs

1. **Endpoints unauthenticated** (ADR-0004) — auth middleware re-added by the auth
   module.
2. **Soft-deleted menus** (RuoYi hard-deletes) — a soft-deleted parent orphans its
   children in the tree view (data remains; `GetMenu` still finds it).
3. **No DB unique on `(parent_id, menu_name)`** — soft biz check only; racy under
   concurrent creates (menus are hand-curated, low risk). Parallels user module.
4. **No full cycle-detection on reparent** — only `parent_id != menu_id` (RuoYi).
   A descendant-cycle orphans nodes in the view, does not crash the builder.
5. **`ext` varchar(256)** not 2000 (locked convention).
6. **Routing columns inert** — stored now, consumed only when `getRouters` lands.
7. **`menu_name` filter exact** — substring search deferred.

## Follow-ups (other modules)

- **auth/permission module** — `getRouters` + `RouterVo`, perms matching/
  enforcement, consume is_frame/is_cache/path/component, `checkRouteConfigUnique`.
- **role module** — `sys_role_menu`, `roleMenuTreeselect`, DeleteMenu's
  role-assignment check.
- **data-scope module** — `create_dept` + row filtering.
