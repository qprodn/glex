# Role ↔ Menu (Menu Grant) — Plan (as-built)

**Status:** designed via grill session 2026-08-14 (Q1–Q8, all A); implemented,
builds, `go vet` clean, biz unit tests pass. PG integration + runtime smoke
still pending (same posture as user/menu/role).
**Part of:** glex — RuoYi-Vue-Plus → Go/Kratos rewrite, module-by-module.
This is the role module's later phase: `sys_role_menu` + checkedKeys, deferred
in `.scratch/role/plan.md`.

> Facts: `.scratch/role-menu/factsheet.md` + `docs/db/ruoyi-vue-plus-role-menu.md`
> (independent research, agree). Decisions: grill session Q1–Q8 (this file).
> No ADR — role-module precedent (plan + CONTEXT.md suffices; the deviations
> below are recorded here and in CONTEXT.md).

## Goal

The **Menu Grant** (菜单授权): which Menus a Role carries, stored in
`sys_role_menu` exactly like RuoYi — full-replacement write, delete-then-insert
in one transaction; read returns the role's checkedKeys shaped by
`menu_check_strictly`. **Not** in scope (deferred): perms string resolution
(auth module), combined treeselect endpoint (frontend composes from
`ListMenus` + `ListRoleMenus`), data scope / `sys_role_dept`.

## Decisions (grill Q1–Q8, all A)

| Q | Decision |
|---|---|
| Q1 | Scope = link table + create-with-menu_ids + replace endpoint + read endpoint + delete-role cleanup + delete-menu cleanup + `menu_check_strictly` back on sys_role. No perms queries, no merged treeselect, no data scope. |
| Q2 | Sub-resource REST: `GET`/`PUT /system/role/{role_id}/menus`; `POST /system/role` accepts optional `menu_ids`. RPCs `ListRoleMenus` / `UpdateRoleMenus`. |
| Q3 | Write validation: every menu id must exist and not be soft-deleted (`ROLE_MENU_NOT_FOUND`); silent dedupe; empty list legal (= clear). |
| Q4 | Read contract: all linked ids (buttons included), soft-deleted excluded, `(parent_id, order_num)` order; strictly=true drops any granted menu that is the parent of another granted menu (half- AND fully-checked parents; RuoYi `selectMenuListByRoleId` filter). **Deviation from Plus:** no `sr.status='0'` filter — a disabled role still returns its grants (Plus's join quirk; effective-perms filtering belongs to the auth module). |
| Q5 | DeleteRole: same-tx hard delete of links + soft delete of role (RuoYi parity: grants are lost for good). |
| Q6 | DeleteMenu: same-tx menu soft delete + hard delete of its role links (Plus `deleteByMenuIds` parity; menu module change in this phase). |
| Q7 | `menu_check_strictly` written only via `UpdateRoleMenus` (with `menu_ids`, one atomic permission save); `Role` message carries it, `GetRole`/`ListRoles` return it, `UpdateRole` PATCH ignores it (Menu.children read-only precedent); `CreateRoleRequest` may carry it. |
| Q8 | proto3 `optional bool` (presence); absent → true (RuoYi column default 1); explicit false disables linkage. |

Other locked semantics: disabled menus (status=1) may be granted (Q3 checks
existence/deletion only, RuoYi parity); stopped roles' grants read back
normally (Q4).

## API

- **proto** `api/system/role/v1/role.proto`:
  - `Role.menu_check_strictly = 11` (bool, output).
  - `CreateRoleRequest`: + `menu_ids` (repeated, optional), `menu_check_strictly` (optional bool).
  - `RoleMenus { repeated int64 menu_ids }` — grant response/echo.
  - `ListRoleMenusRequest { role_id }` → `GET /system/role/{role_id}/menus`.
  - `UpdateRoleMenusRequest { role_id, menu_ids, optional menu_check_strictly }` → `PUT /system/role/{role_id}/menus` (body `*`), returns `RoleMenus` (persisted, deduped ids).
- **error reason** `ROLE_MENU_NOT_FOUND = 5` → `biz.ErrRoleMenuNotFound` (BadRequest).

## Layering

- **biz** — `role.go`: `Role.MenuCheckStrictly`; `RoleMenuLink{MenuID, ParentID}`
  read projection; `RoleRepo` + `CreateRole(ctx, r, menuIDs)`,
  `ListRoleMenuLinks`, `ReplaceRoleMenus(ctx, roleID, menuIDs, strict)`;
  `RoleUsecase` now takes `MenuRepo` (validation via `MenuRepo.ListByIDs`;
  length mismatch → `ErrRoleMenuNotFound`); `ListRoleMenus` computes the
  strictly filter in biz (unit-testable); `UpdateRoleMenus` = existence check
  → dedupe/validate → `ReplaceRoleMenus`, returns persisted ids. `menu.go`:
  `MenuRepo.ListByIDs` (active rows only). `errors.go`: `ErrRoleMenuNotFound`.
- **data** — `ent/schema/role.go`: `menu_check_strictly bool default true`;
  M2M edge `Role.menus → Menu` annotated `sys_role_menu` (composite PK
  `(role_id, menu_id)`, RuoYi shape — first edge in the repo). `ent/schema/menu.go`:
  inverse edge `Menu.roles`. `role.go` repo: `CreateRole`/`DeleteRole`/
  `ReplaceRoleMenus` wrap `ent.Tx` (`AddMenuIDs` / `ClearMenus` graph
  mutations); `ListRoleMenuLinks` via `Role.QueryMenus()` with
  `menu.DeletedAtIsNil()` + `Order(ByParentID, ByOrderNum)`. `menu.go` repo:
  `ListByIDs`; `DeleteMenu` wraps tx + `ClearRoles`.
- **service** — `role.go`: `CreateRole` passes `menu_ids` + presence-defaulted
  flag; `ListRoleMenus` / `UpdateRoleMenus` handlers; `convertRole` carries the
  flag. `UpdateRole` never maps the flag (PATCH-ignored).
- **wiring** — `NewRoleUsecase(repo, menuRepo)` (wire regenerated). HTTP/gRPC
  registration unchanged (same service constructor).

## Schema (`docs/db/schema.sql`, aligned with ent)

```sql
-- on sys_role
menu_check_strictly boolean not null default true,  -- 菜单树选择项是否关联显示

create table if not exists sys_role_menu (
    role_id bigint not null,
    menu_id bigint not null,
    constraint sys_role_menu_pkey primary key (role_id, menu_id)
);
```

## Testing

- biz unit tests (fake role + menu repos, in-memory): create with menus /
  unknown menu rejected (no partial create) / dedupe; UpdateRoleMenus
  replace + clear + unknown rejected + role-not-found; ListRoleMenus strict
  drops half-checked parent, strict drops fully-checked parent, non-strict
  verbatim, role-not-found; existing CRUD tests migrated to the new
  `NewRoleUsecase(repo, menuRepo)` signature.
- data: PG integration pending (repo posture).
- acceptance: create→grant→read→replace→delete pending runtime.

## Known trade-offs

1. **Delete-role destroys grants irrecoverably** (RuoYi parity) — a role
   soft-delete clears its links; there is no restore flow.
2. **Check-then-write race on menu ids** — a menu soft-deleted between
   validation and insert leaves a dangling link row; reads filter it
   (`menu.DeletedAtIsNil()`), so it is invisible and inert. Accepted (same
   class as the repo's uniqueness races).
3. **`menu_check_strictly` PATCH-ignored** — UpdateRole silently drops the
   field from the mask (children precedent). Client must use
   `UpdateRoleMenus` to change it; a wrong guess at semantics is caught by
   tests, not by the API.
4. **First ent edge in the repo** — new generated surface
   (`role.Menus`/`menu.Roles`, `sys_role_menu` join table). Dev auto-migrate
   (`WithDropIndex(true)`) creates the table; production is hand-managed via
   `schema.sql` (discipline, per ADR).
5. **Deviation from Plus: no role-status filter on read** (Q4) — a disabled
   role's grants read back fully; Plus's `sr.status='0'` join would return
   empty. Intentional: the flag is the role's own config; effective-perms
   filtering is the auth module's job.

## Follow-ups (other modules)

- **auth module** — resolve grants → perms (`selectMenuPermsByRoleId`/`ByUserId`
  equivalents, `sr.status='0'` filter there), user↔role, super-admin
  bypass (`*:*:*`), audit fill, online-session kick on permission writes.
- **data-scope module** — `data_scope` + `sys_role_dept` (mirror of this
  pattern) + `deptTree`.
- **menu module** — nothing left here; DeleteMenu cleanup landed in this phase.
