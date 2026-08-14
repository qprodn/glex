# RuoYi-Vue-Plus: user ↔ role assignment — API surface & semantics

Research against primary sources (Java sources + SQL + official Vue UI), branch **6.X** (current default; `master` on GitHub redirects to 6.X). Version deltas vs 5.X noted where they matter (5.X used `userId=1`/`roleId=1`; 6.X uses snowflake IDs).

## 1. `sys_user_role` table (from `script/sql/ry_vue.sql`)

```sql
create table sys_user_role (
    user_id   bigint(20) not null comment '用户ID',
    role_id   bigint(20) not null comment '角色ID',
    primary key(user_id, role_id),
    key idx_sys_user_role_rid (role_id)
) engine=innodb comment = '用户和角色关联表';
```

- Composite **primary key (user_id, role_id)** → uniqueness on the pair is enforced by the PK (there is no separate unique index; the PK *is* it).
- Secondary index on `role_id` for reverse lookups.
- **No foreign keys anywhere** in the schema (`grep -c "foreign key"` → 0). All referential integrity is application-level.
- Seed: `insert into sys_user_role values (1761100000000000001, 1761300000000000001)` (admin → superadmin), plus a few normal users.
- Entity `SysUserRole` (`org.dromara.system.domain.SysUserRole`): only `userId` (`@TableId(type = IdType.INPUT)`) and `roleId`; no id column, no timestamps.

## 2. Endpoint surface

All responses are wrapped in `R<T>` (`code/msg/data`); lists are `PageResult<T>` (page, pageSize, total). Mutating endpoints carry `@Log` (audit) + `@RepeatSubmit` (idempotency guard) + `@SaCheckPermission`.

| Method | Path | Params | Semantics |
|---|---|---|---|
| GET | `/system/user/authRole/{userId}` | path `userId` | User + full assignable role list; each role has `flag=true` if already assigned. Superadmin role is filtered out of the list unless the target user *is* the superadmin. Perm: `system:user:query`. |
| PUT | `/system/user/authRole` | query `userId`, `roleIds` (`Long[]`, comma-separated) — **not** a JSON body | **Full replacement** of the user's role set (`insertUserAuth` → `insertUserRole(userId, roleIds, clear=true)`). Perm: `system:user:edit`. |
| GET | `/system/user/{userId}` | path `userId` | User edit-dialog data: `user` (SysUserVo), `roleIds` (`List<Long>`), `roles` (all normal roles), `postIds`, `posts`. Perm: `system:user:query`. |
| GET | `/system/role/authUser/allocatedList` | query: `SysUserBo` (`roleId` = the role to filter by, `userName`, `phoneNumber`, `status`) + `PageQuery` | Users that **have** the given role (inner join on `sys_user_role` + `sys_role`, `r.role_id = :roleId`). Perm: `system:role:list`. |
| GET | `/system/role/authUser/unallocatedList` | same query shape | Users that do **not** have the role: `selectUserIdsByRoleId(roleId)` then `user_id NOT IN (...)`. Perm: `system:role:list`. |
| PUT | `/system/role/authUser/selectAll` | query `roleId`, `userIds` (`Long[]`) | **Incremental add**: batch-inserts `(roleId, userId)` rows. No clear, no dedup (PK rejects dupes). Perm: `system:role:edit`. |
| PUT | `/system/role/authUser/cancel` | JSON body `SysUserRole` (`{userId, roleId}`) | Deletes one `(roleId, userId)` row. Perm: `system:role:edit`. |
| PUT | `/system/role/authUser/cancelAll` | query `roleId`, `userIds` (`Long[]`) | **Incremental remove**: deletes rows where `role_id=:roleId AND user_id IN (:userIds)`. Perm: `system:role:edit`. |

Both list endpoints apply `@DataPermission` (dept `dept_id`, `create_by` data-scope) and paginate.

## 3. Request/response DTO shapes

- `SysUserInfoVo` (`org.dromara.system.domain.vo`): `user: SysUserVo`, `roleIds: List<Long>`, `roles: List<SysRoleVo>`, `postIds: List<Long>`, `posts: List<SysPostVo>`. Used by both `GET /system/user/{userId}` and `GET /system/user/authRole/{userId}` (authRole leaves `roleIds` unset; the *checked* state rides on `SysRoleVo.flag`).
- `SysRoleVo`: `roleId, roleName, roleKey, roleSort, dataScope, menuCheckStrictly, deptCheckStrictly, status, remark, createTime` + `boolean flag = false` ("用户是否存在此角色标识" — the checked flag) + `isSuperAdmin()` (roleId == SUPER_ADMIN_ROLE_ID).
- `SysUserVo`: `userId, deptId, userName, nickName, userType, email, phoneNumber, gender, avatar, password(@JsonIgnore), status, loginIp, loginDate, remark, createTime, updateTime, deptName` + `roles: List<SysRoleVo>`, `roleIds: Long[]`, `postIds: Long[]`, `roleId` (the *current-role* filter used by the two authUser list endpoints).
- `SysUserBo` (write side): same fields + `roleIds: Long[]` with `@Size(min = 1, message = "用户角色不能为空")` (required on create/edit), `postIds`, `roleId`, `userIds`, `excludeUserIds`, `params`.

**There is no `SysUserListVo` and no `roleGroup` string field in Plus.** The list endpoint returns `SysUserVo` rows without roles populated; role data is fetched per-user via the detail/authRole endpoints. The classic RuoYi-Vue `SysUser.roleGroup` (GROUP_CONCAT of role names) does not exist here, although a service helper `selectUserRoleGroup(userId)` (comma-joined role names) survives in `ISysUserService` but is not wired into these endpoints.

## 4. Replacement vs incremental; cascade

- **User → roles (user side): full replacement.** `SysUserServiceImpl.insertUserAuth(userId, roleIds)` → private `insertUserRole(userId, roleIds, clear=true)`: deletes all `sys_user_role` rows for the user, then batch-inserts the new set. The user-edit endpoint (`PUT /system/user`) also full-replaces roles (`insertUserRole(user, true)`) from `SysUserBo.roleIds`.
  - **Edge case:** if `roleIds` is empty, `insertUserRole` early-returns and does **not** clear — an empty array is a no-op, not "remove all". (The BO's `@Size(min=1)` makes empty impossible through the edit form, but `PUT /authRole` has no such validation.)
- **Role → users (role side): incremental.** `insertAuthUsers(roleId, userIds)` only inserts; `deleteAuthUsers(roleId, userIds)` / `deleteAuthUser(userRole)` only delete. No full-resync on the role side.
- **User deletion:** `deleteUserByIds` (used by `DELETE /system/user/{userIds}`) first deletes `sys_user_role` rows (`userRoleMapper...in(userId, ids).delete()`), then `sys_user_post`, then the user — application-level cascade, `@Transactional`.
- **Role deletion:** `deleteRoleByIds` (used by `DELETE /system/role/{roleIds}`) first checks `countUserRoleByRoleId(roleId) > 0` → throws `"<roleName>已分配，不能删除!"`. **A role assigned to any user cannot be deleted.** It then removes `sys_role_menu` / `sys_role_dept` rows and the role. Note `deleteRoleById` (single) does *not* check user count, but it's only invoked from internal code, not the controller.
- Related guard: a role **cannot be disabled** while assigned — `updateRoleStatus`/`updateRoleBaseInfo` throw `"角色已分配，不能禁用!"` when `status="1"` and `countUserRoleByRoleId > 0`.

## 5. Superadmin handling

- IDs (6.X, `SystemConstants`): `SUPER_ADMIN_USER_ID = 1761100000000000001L`, `SUPER_ADMIN_ROLE_ID = 1761300000000000001L`, `SUPER_ADMIN_ROLE_KEY = "superadmin"`. (5.X: `SUPER_ADMIN_ID = 1L` for both user and role — the classic `userId=1` you asked about; semantics are identical.) `LoginHelper.isSuperAdmin(userId)` is purely an ID equality check.
- `checkUserAllowed(userId)` throws `"不允许操作超级管理员用户"` — applied in user `edit`, `resetPwd`, `changeStatus`, and `deleteUserByIds`. So the admin's record cannot be edited/disabled/reset/deleted via the normal endpoints.
- **`PUT /system/user/authRole` does NOT call `checkUserAllowed`** — only `checkUserDataScope`. The service compensates: `insertUserRole` strips `SUPER_ADMIN_ROLE_ID` from the submitted set when the target user is *not* superadmin, and throws `"不允许为普通用户分配超级管理员角色"` if nothing remains. So a normal user can never be *granted* the superadmin role. Conversely, when the target *is* the superadmin the role set can technically be replaced (even dropping superadmin) — the API relies on the frontend hiding that row (authRole dialog renders `row.flag` toggles and the backend list filter omits superadmin for non-admin targets).
- The superadmin **role** itself is protected by `checkRoleAllowed(role)`: add/edit/delete/disable of roleId = SUPER_ADMIN_ROLE_ID → `"不允许操作超级管理员角色"`; using/renaming to the `"superadmin"` roleKey → `"不允许使用系统内置管理员角色标识符!"`.
- All `authRole`-family mutations also guard **self-modification**: `insertAuthUsers` / `deleteAuthUser(s)` throw `"不允许修改当前用户角色!"` if the current login user's own binding is touched.

## 6. Role-selection UX data (confirmed against official UI, `CrazyLionCat/plus-ui`)

- User edit dialog: `GET /system/user/{userId}` → `form.roleIds = data.roleIds` (multi-select value), role options merged from `data.roles` + `data.user.roles` (deduped by roleId). So the checked-state source for editing is **`SysUserInfoVo.roleIds`**; the assignable list is `roles`.
- Assign dialog (`authRole.vue`): `GET /system/user/authRole/{userId}` → table rows of `roles`, `row.flag` drives checkbox state; on submit `PUT /system/user/authRole` with `{ userId, roleIds: ids.join(',') }` as **query params** (confirmed in `src/api/system/user/index.ts`).
- Role dialog (`authUser.vue` side): two GET lists (allocated/unallocated) rendered as two tables; `selectAll`/`cancelAll` move rows between them, `cancel` removes a single row.

## 7. Uniqueness & FK behavior

- **Unique on (user_id, role_id)** via the composite primary key — no separate unique index needed. Duplicate inserts (possible via role-side `insertAuthUsers`, which does no dedup) raise a duplicate-key error inside the `@Transactional` method.
- **No FK constraints**; `roleId` validity on the user side is validated in `insertUserRole` by `roleMapper.selectRoleCount(roleList) != roleList.size()` → throws `"没有权限访问角色的数据"` (covers both existence *and* data scope). Role-side `insertAuthUsers` does **not** validate that the `userIds` exist — rows for nonexistent users are only prevented by application flow, not by the DB.

## Porting notes for a Go Kratos service

- One table, composite PK `(user_id, role_id)`, index on `role_id`; no FKs — enforce integrity in biz.
- User-side assign = **transactional delete-then-insert** (full replacement); role-side = pure insert/delete (incremental). Guard the empty-roleIds no-op vs clear semantics explicitly.
- Enforce: no superadmin role grants to non-admin; no role delete/disable while assigned; no self-modification of own binding; data-scope checks on every read/write; `flag` field for the assign dialog, `roleIds` array for the edit dialog.
- Cascade user deletion to `sys_user_role` in the same transaction; role deletion is blocked (soft-guard) rather than cascaded.

## Primary sources

- [SysUserController.java (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/controller/system/SysUserController.java)
- [SysRoleController.java (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/controller/system/SysRoleController.java)
- [SysUserServiceImpl.java (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysUserServiceImpl.java)
- [SysRoleServiceImpl.java (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysRoleServiceImpl.java)
- [SysUserMapper.java (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/mapper/SysUserMapper.java) · [SysUserRoleMapper.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/mapper/SysUserRoleMapper.java) · [SysUserRole.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysUserRole.java)
- VOs/BOs: [SysUserVo](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/vo/SysUserVo.java) · [SysUserInfoVo](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/vo/SysUserInfoVo.java) · [SysRoleVo](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/vo/SysRoleVo.java) · [SysUserBo](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/bo/SysUserBo.java)
- Constants/helpers: [SystemConstants.java (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-common/ruoyi-common-core/src/main/java/org/dromara/common/core/constant/SystemConstants.java) (5.X `SUPER_ADMIN_ID = 1L` in the [5.X branch](https://github.com/dromara/RuoYi-Vue-Plus/blob/5.X/ruoyi-common/ruoyi-common-core/src/main/java/org/dromara/common/core/constant/SystemConstants.java)) · [LoginHelper.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/ruoyi-common/ruoyi-common-satoken/src/main/java/org/dromara/common/satoken/utils/LoginHelper.java)
- Schema: [script/sql/ry_vue.sql (6.X)](https://github.com/dromara/RuoYi-Vue-Plus/blob/6.X/script/sql/ry_vue.sql)
- UI (official Vue3 ElementPlus frontend): [authRole.vue](https://github.com/CrazyLionCat/plus-ui/blob/master/src/views/system/user/authRole.vue) · [user/index.vue](https://github.com/CrazyLionCat/plus-ui/blob/master/src/views/system/user/index.vue) · [api/system/user/index.ts](https://github.com/CrazyLionCat/plus-ui/blob/master/src/api/system/user/index.ts) · [api/system/role/index.ts](https://github.com/CrazyLionCat/plus-ui/blob/master/src/api/system/role/index.ts)
