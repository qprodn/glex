# Menu module scope — CRUD + tree, routing and role-menu deferred

The menu module implements only **`sys_menu` CRUD + an in-memory menu tree**.
`getRouters` (the current-user route tree) and `sys_role_menu` /
`roleMenuTreeselect` are deferred. Decided 2026-08-13 at Menu module planning.

RuoYi-Vue-Plus's `SysMenu` surface spans three layers: (a) `sys_menu` table CRUD
and tree queries, (b) the `sys_role_menu` join plus `roleMenuTreeselect` (menu
tree + checked ids for a role), and (c) `getRouters`, which builds the frontend
route tree filtered by the *current user's* permissions (joining `sys_role_menu`
→ `sys_user_role` → `sys_role`, matching `perms`, stripping buttons, emitting
`RouterVo`). Layers (b) and (c) are not buildable in glex yet: (c) needs a
current user and a resolved permission set, which require the auth module removed
in [ADR-0004](./0004-remove-template-admin-and-auth.md); (b) needs a role entity
and `role_id`, which require the role module. Shipping half-working endpoints for
non-existent modules would violate the write→test→review→next-module cadence —
`getRouters` cannot be tested meaningfully until auth exists.

We therefore scope the module to (a) only, and model `sys_menu`'s **complete**
structure (routing columns included) so that `getRouters` can read the same table
later with no schema migration. Storing intrinsic fields on an entity we are
creating is not premature infrastructure; the line we hold is *complete entity,
minimal surface* — no `getRouters` or `role_menu` endpoints.

**Tree strategy:** the repo returns a flat list (AIP filter + order by
`parent_id, order_num`); biz builds the tree in memory in a single pass (roots =
`parent_id == 0`, children grouped by parent, sorted by `order_num`). No
recursive SQL/CTE. Menus are a small, hand-curated set (tens to low-hundreds of
rows).

**Related decisions:** `menu_type` is a proto enum stored as `smallint`
(char(1)→smallint per [ADR-0003](./0003-postgresql.md)); menus are soft-deleted
via `deleted_at` for repo-wide consistency (RuoYi hard-deletes — a noted
deviation: a soft-deleted parent orphans its children in the tree view); and
`(parent_id, menu_name)` carries no DB unique constraint, only a soft biz check.

Consequences: all menu endpoints are currently unauthenticated (ADR-0004);
`perms`, `is_frame`, `is_cache`, `path`, and `component` are stored but inert
until the auth module consumes them.
