# User↔role association — auth module owns it, full replacement, asymmetric deletion

The user↔role link (**Role Assignment**, 角色分配, table `sys_user_role`) is
implemented in a new **auth module** (`api/iam/auth/v1`,
`internal/application/iam.AuthUsecase`),
not in the user or role modules — even though its routes are path-nested under
`/iam/user/{user_id}/roles` and `/iam/role/{role_id}/users`. Decided at
Role Assignment planning (grill-with-docs session).

RuoYi-Vue-Plus exposes two sides: user-side assignment (`authRole` — full
replacement of a user's role ids) and role-side user management
(`allocatedList`/`unallocatedList`/`selectAll`/`cancelAll` — incremental
add/remove). glex ports the shape but **not the incremental verbs**: both
directions are read-only or full-replacement (`ListUserRoles` /
`UpdateUserRoles` / `ListRoleUsers`), matching the Menu Grant precedent
(`PUT /iam/role/{role_id}/menus`). `selectAll`/`cancelAll` are deferred
until a real UX needs batch moves.

CONTEXT.md previously deferred assignment "to the auth module"; ADR-0004 had
removed the template's auth. The module now exists *for the link* and will host
login/session/permission resolution later — but today there is no authn/authz
anywhere, so all endpoints stay unauthenticated.

**Why auth owns it:** the link is identity/access data, not user data or role
data; user/role modules would otherwise grow cross-imports of each other's
repos (the role module already guards delete/disable via the auth repo
interface, `CountUserRoles`). Path nesting under the resource is pure HTTP
routing.

**Write semantics:** full replacement, empty list = clear, duplicates silently
deduped — identical to Menu Grant. RuoYi's `authRole` treats an empty `roleIds`
as a no-op (not a clear); that is a defensive-code side effect, not a design,
and glex deliberately does not copy it.

**Asymmetric deletion (deliberate deviation from the Menu Grant precedent):**
deleting a *user* hard-deletes its links in the same transaction (RuoYi
semantics; links are meaningless without the user). Deleting or *disabling* a
*role* that still has users assigned is rejected (`ROLE_ASSIGNED`) — a role is a
reusable permission bundle, and silently revoking N users' permissions is a
dangerous cascade. Menu Grant, by contrast, cascades on role delete: menus are
part of the role's own configuration, users are external holders.

**Error reasons:** auth defines no `error_reason.proto`; it reuses
`USER_NOT_FOUND` / `ROLE_NOT_FOUND` from the owning modules. `ROLE_ASSIGNED`
was added to the role module's proto since
the guard lives in the role usecase.

**Consistency details:** link rows are hard-deleted (no timestamps/soft
delete), like `sys_role_menu`; there is **no separate index on `role_id`**
(RuoYi has one) because dev auto-migration runs `migrate.WithDropIndex(true)`,
which would drop a hand-added index, and role-side lookups are EXISTS scans
over a small back-office join table.

Consequences: `DeleteUser` now opens a transaction (was a single update); the
role usecase constructor gained a `UserRoleRepo` parameter; validation of
role ids in `UpdateUserRoles` reuses the freshly-read active-role set (no
second query, unlike Menu Grant's `ListByIDs` pattern); all endpoints remain
unauthenticated until the auth module grows login.
