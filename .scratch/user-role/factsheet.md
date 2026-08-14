# RuoYi-Vue-Plus — User & Role Management: Reference Fact Sheet

**Version studied:** RuoYi-Vue-Plus **v6.0.0** (matches upstream `master` — every
file below was byte-compared against `raw.githubusercontent.com/dromara/RuoYi-Vue-Plus/master/...`
and is identical; local checkout at `RuoYi-Vue-Plus/`).

**Source layout (v5/v6):** controllers live in the **system module**, not
`ruoyi-admin` (the `ruoyi-admin/.../SysUserController.java` path in old docs/forks
is the pre-5.x layout). All paths below are under
`ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/`.

- [SysUserController.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/controller/system/SysUserController.java) · [SysRoleController.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/controller/system/SysRoleController.java)
- `domain/`: [SysUser.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysUser.java) · [SysRole.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysRole.java) · [SysUserRole.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysUserRole.java) · [SysRoleMenu.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysRoleMenu.java) · [SysRoleDept.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysRoleDept.java)
- `service/impl/`: [SysUserServiceImpl.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysUserServiceImpl.java) · [SysRoleServiceImpl.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysRoleServiceImpl.java)
- `mapper/`: [SysUserMapper.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/mapper/SysUserMapper.java) · [SysRoleMapper.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/mapper/SysRoleMapper.java) · [SysUserRoleMapper.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/mapper/SysUserRoleMapper.java)
- Schema: [script/sql/ry_vue.sql](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/script/sql/ry_vue.sql) (MySQL) · [postgres_ry_vue.sql](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/script/sql/postgres/postgres_ry_vue.sql) (PostgreSQL) · `script/sql/oracle/oracle_ry_vue.sql`, `script/sql/sqlserver/sqlserver_ry_vue.sql`
- `ruoyi-common/ruoyi-common-mybatis/.../core/domain/BaseEntity.java` · `ruoyi-common/ruoyi-common-core/.../constant/SystemConstants.java` · `ruoyi-common/ruoyi-common-mybatis/.../enums/DataScopeType.java`
- Official docs (6.X): [数据库表设计](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/database.md) · [权限控制](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions_control.md) · [数据权限](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions.md) · [系统用户相关](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/user.md)

---

## 1. Table schemas

All DDL from [ry_vue.sql](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/script/sql/ry_vue.sql);
PostgreSQL variant in `script/sql/postgres/postgres_ry_vue.sql` (identical
columns; `int8`/`timestamp`/`varchar(n)`; join tables get named `sys_user_role_pk`
constraints and separate `create index` statements).

### 1.1 `sys_user`

```sql
create table sys_user (
    user_id           bigint(20)      not null                   comment '用户ID',
    dept_id           bigint(20)      default null               comment '部门ID',
    user_name         varchar(30)     not null                   comment '用户账号',
    nick_name         varchar(30)     not null                   comment '用户昵称',
    user_type         varchar(10)     default 'sys_user'         comment '用户类型（sys_user系统用户）',
    email             varchar(50)     default ''                 comment '用户邮箱',
    phone_number      varchar(11)     default ''                 comment '手机号码',
    gender            char(1)         default '0'                comment '用户性别（0男 1女 2未知）',
    avatar            bigint(20)                                 comment '头像地址（OSS 对象ID）',
    password          varchar(100)    default ''                 comment '密码（BCrypt hash）',
    status            char(1)         default '0'                comment '账号状态（0正常 1停用）',
    del_flag          char(1)         default '0'                comment '删除标志（0存在 1删除）',
    login_ip          varchar(128)    default ''                 comment '最后登录IP',
    login_date        datetime                                   comment '最后登录时间',
    create_dept       bigint(20)      default null               comment '创建部门',
    create_by         bigint(20)      default null               comment '创建者（用户ID）',
    create_time       datetime                                   comment '创建时间',
    update_by         bigint(20)      default null               comment '更新者（用户ID）',
    update_time       datetime                                   comment '更新时间',
    remark            varchar(500)    default null               comment '备注',
    primary key (user_id),
    key idx_sys_user_dept_id   (dept_id),
    key idx_sys_user_create_by (create_by),
    key idx_sys_user_user_name (user_name),
    key idx_sys_user_phone     (phone_number)
) engine=innodb comment = '用户信息表';
```

Key facts:
- **No `tenant_id`** in v6 (v5.x had one; v6 removed multi-tenancy).
- Indexes on `user_name`/`phone_number` are **non-unique** — uniqueness is
  enforced in application code only (see §4.5). Same for `email` (no index at all).
- `del_flag` is a **logic-delete** flag mapped with MyBatis-Plus `@TableLogic`
  on `SysUser.delFlag`; `deleteById(s)` become `UPDATE ... SET del_flag='1'`,
  and every generated query appends `del_flag = '0'`.
- `create_by`/`update_by` are **bigint user IDs** (Plus style; classic RuoYi
  used varchar login names). `create_dept` is a Plus addition.
- IDs are **snowflake** (seed admin `1761100000000000001`), not auto-increment.
- Java mapping ([SysUser.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysUser.java)): `userId` `@TableId(value="user_id")`; `password` has `@TableField(insertStrategy/updateStrategy/whereStrategy = NOT_EMPTY)` (empty password never written); `avatar` is `Long` (OSS id); `gender`/`status`/`delFlag`/`userType` are `String`; extends `BaseEntity`.

### 1.2 `sys_role`

```sql
create table sys_role (
    role_id              bigint(20)      not null                   comment '角色ID',
    role_name            varchar(30)     not null                   comment '角色名称',
    role_key             varchar(100)    not null                   comment '角色权限字符串',
    role_sort            int(4)          not null                   comment '显示顺序',
    data_scope           char(1)         default '1'                comment '数据范围（1：全部 2：自定 3：本部门 4：本部门及以下 5：仅本人 6：部门及以下或本人）',
    menu_check_strictly  tinyint(1)      default 1                  comment '菜单树选择项是否关联显示',
    dept_check_strictly  tinyint(1)      default 1                  comment '部门树选择项是否关联显示',
    status               char(1)         not null                   comment '角色状态（0正常 1停用）',
    del_flag             char(1)         default '0'                comment '删除标志（0存在 1删除）',
    create_dept          bigint(20)      default null               comment '创建部门',
    create_by            bigint(20)      default null               comment '创建者',
    create_time          datetime                                   comment '创建时间',
    update_by            bigint(20)      default null               comment '更新者',
    update_time          datetime                                   comment '更新时间',
    remark               varchar(500)    default null               comment '备注',
    primary key (role_id),
    key idx_sys_role_create_dept (create_dept),
    key idx_sys_role_create_by   (create_by)
) engine=innodb comment = '角色信息表';
```

Key facts: no unique index on `role_name`/`role_key` (code-enforced, §4.5);
`del_flag` logic delete (`@TableLogic` on `SysRole.delFlag`); `menuCheckStrictly`/`deptCheckStrictly` are `Boolean` (tinyint(1)); `roleSort` `Integer`; `dataScope`/`status`/`delFlag` `String`; extends `BaseEntity`.

### 1.3 `sys_user_role` — **composite PK** (user, role)

```sql
create table sys_user_role (
    user_id   bigint(20) not null comment '用户ID',
    role_id   bigint(20) not null comment '角色ID',
    primary key(user_id, role_id),
    key idx_sys_user_role_rid (role_id)
) engine=innodb comment = '用户和角色关联表';
```

Postgres: `constraint sys_user_role_pk primary key (user_id, role_id)` +
`create index idx_sys_user_role_rid ON sys_user_role (role_id);`
No audit columns, no `del_flag` (hard delete of rows). Java
([SysUserRole.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/SysUserRole.java)):
`@TableId(type = IdType.INPUT)` on `userId` (MyBatis-Plus only models the first
PK column; the DB enforces the composite PK).

### 1.4 `sys_role_menu` — **composite PK** (role, menu)

```sql
create table sys_role_menu (
    role_id   bigint(20) not null comment '角色ID',
    menu_id   bigint(20) not null comment '菜单ID',
    primary key(role_id, menu_id)
) engine=innodb comment = '角色和菜单关联表';
```

### 1.5 `sys_role_dept` — **composite PK** (role, dept)

```sql
create table sys_role_dept (
    role_id   bigint(20) not null comment '角色ID',
    dept_id   bigint(20) not null comment '部门ID',
    primary key(role_id, dept_id)
) engine=innodb comment = '角色和部门关联表';
```

`sys_role_dept` stores the custom dept list for `data_scope = '2'` (自定义).

Seed rows ([ry_vue.sql](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/script/sql/ry_vue.sql)): user `1761100000000000001` (`admin`, bcrypt hash of the standard admin password) bound to role `1761300000000000001` (`超级管理员`/`superadmin`, data_scope 1); test users/roles use `...0003`/`...0004` with data_scope 4/5.

---

## 2. User-management endpoints

Base path: `@RequestMapping("/system/user")`. **Permission annotation is Sa-Token's
`@SaCheckPermission("system:user:xxx")` — NOT Spring's `@PreAuthorize("@ss.hasPermi(...)")`**
(classic RuoYi syntax; Plus switched to Sa-Token; see [权限控制 docs](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions_control.md)).
Global auth: Sa-Token login check + `clientid` header validation; all endpoints
require login, endpoints listed without `@SaCheckPermission` require login only.

| Method + Path | Permission / extras | Request | Response |
|---|---|---|---|
| `GET /system/user/list` | `system:user:list` | query: `SysUserBo` (userName, nickName, status, phoneNumber, deptId, userIds, excludeUserIds, beginTime, endTime) + `PageQuery` (pageNum, pageSize, orderByColumn, isAsc) | `R<PageResult<SysUserVo>>` |
| `POST /system/user/export` | `@Log(EXPORT)` `system:user:export` | query SysUserBo | Excel stream |
| `POST /system/user/importData` | `@Log(IMPORT)` `system:user:import` | `multipart/form-data` file + `updateSupport` | `R<Void>` (analysis result) |
| `POST /system/user/importTemplate` | none | — | Excel template stream |
| `GET /system/user/getInfo` | none (login only) | — | `R<UserInfoVo>` `{user, permissions[], roles[]}` |
| `GET /system/user` **and** `GET /system/user/{userId}` | `system:user:query` | `@GetMapping({"/", "/{userId}"})`, userId optional | `R<SysUserInfoVo>` `{user, roleIds[], roles[], postIds[], posts[]}` |
| `POST /system/user` | `system:user:add` `@Log(INSERT)` `@RepeatSubmit` | JSON body `SysUserBo` (@Validated) | `R<Void>` |
| `PUT /system/user` | `system:user:edit` `@Log(UPDATE)` `@RepeatSubmit` | JSON body `SysUserBo` (@Validated) | `R<Void>` |
| `DELETE /system/user/{userIds}` | `system:user:remove` `@Log(DELETE)` | path `Long[]` (comma-joined, e.g. `/system/user/1,2,3`) | `R<Void>` |
| `GET /system/user/optionselect` | `system:user:query` | query `userIds[]` (optional), `deptId` (optional) | `R<List<SysUserVo>>` |
| `PUT /system/user/resetPwd` | `@ApiEncrypt` `system:user:resetPwd` `@Log(UPDATE)` `@RepeatSubmit` | JSON body `SysUserBo` `{userId, password}` — body is **AES-encrypted** (API-encrypt feature) | `R<Void>` |
| `PUT /system/user/changeStatus` | `system:user:edit` `@Log(UPDATE)` `@RepeatSubmit` | JSON body `SysUserBo` `{userId, status}` | `R<Void>` |
| `GET /system/user/unlock/{userId}` | `system:user:edit` `@Log(OTHER)` | path userId | `R<Void>` (deletes `pwd_err_cnt:<userName>` Redis key) |
| `GET /system/user/authRole/{userId}` | `system:user:query` | path userId | `R<SysUserInfoVo>` `{user, roles[]}` — each role has `flag=true` if assigned |
| `PUT /system/user/authRole` | `system:user:edit` `@Log(GRANT)` `@RepeatSubmit` | **query params** `userId` + `roleIds` (Long[] — comma-joined or repeated) | `R<Void>` |
| `GET /system/user/deptTree` | `system:user:list` | query SysDeptBo | `R<List<Tree<Long>>>` |
| `GET /system/user/list/dept/{deptId}` | `system:user:list` | path deptId | `R<List<SysUserVo>>` |

Request DTO [SysUserBo](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/bo/SysUserBo.java):
`userId, deptId, userName(@NotBlank @Size 2-30 @Xss), nickName(@NotBlank @Size≤30 @Xss), userType, email(@Email @Size≤50), phoneNumber, gender, avatar(Long), password, status, remark, roleIds(Long[] @Size(min=1)), postIds(Long[]), roleId, userIds(String), excludeUserIds(String), createBy, updateBy, params(Map)`.
`@Size(min=1)` on `roleIds` passes when null (Bean Validation skips null), so roleIds is effectively optional but must have ≥1 element when present.

Response VO [SysUserVo](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/vo/SysUserVo.java):
`userId, deptId, userName, nickName, userType, email(@Sensitive EMAIL, perms=system:user:edit), phoneNumber(@Sensitive PHONE), gender, avatar(Long), avatarUrl(translated from OSS id), password(@JsonIgnore+@JsonProperty → write-only, never serialized), status, loginIp, loginDate, remark, createTime, updateTime, deptName(translated), roles[], roleIds[], postIds[], roleId`.

Response envelope `R<T>` `{code, msg, data}`: `200` success / `500` error / `601` warn
([R.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-common/ruoyi-common-core/src/main/java/org/dromara/common/core/domain/R.java)).
`PageResult<T>` = `{total: long, rows: Collection<T>}` ([PageResult.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-common/ruoyi-common-core/src/main/java/org/dromara/common/core/domain/PageResult.java)).
`SysUserInfoVo` = `{user, roleIds(List<Long>), roles(List<SysRoleVo>), postIds, posts}`.
`UserInfoVo` (getInfo) = `{user, permissions(Set<String>), roles(Set<String> roleKeys)}` from the login session.

Controller-level guards: `add` → `deptService.checkDeptDataScope(deptId)` + uniqueness checks + `BCrypt.hashpw`; `edit` → `checkUserAllowed` + `checkUserDataScope` + `checkDeptDataScope` + uniqueness; `remove` → **"当前用户不能删除"** if own id is in the list (then service re-checks allowed/data-scope per id); `resetPwd`/`changeStatus` → `checkUserAllowed` + `checkUserDataScope`; `authRole` (GET) → `checkUserDataScope`; `authRole` (PUT) → `checkUserDataScope` only.

---

## 3. Role-management endpoints

Base path: `@RequestMapping("/system/role")`.

| Method + Path | Permission / extras | Request | Response |
|---|---|---|---|
| `GET /system/role/list` | `system:role:list` | query: `SysRoleBo` (roleName, roleKey, status, beginTime, endTime) + `PageQuery` | `R<PageResult<SysRoleVo>>` |
| `POST /system/role/export` | `@Log(EXPORT)` `system:role:export` | query SysRoleBo | Excel stream |
| `GET /system/role/{roleId}` | `system:role:query` | path roleId | `R<SysRoleVo>` (after `checkRoleDataScope`) |
| `POST /system/role` | `system:role:add` `@Log(INSERT)` `@RepeatSubmit` | JSON `SysRoleBo` (@Validated) | `R<Void>` |
| `PUT /system/role` | `system:role:edit` `@Log(UPDATE)` `@RepeatSubmit` | JSON `SysRoleBo` — **base info only** (no menu/dept permission) | `R<Void>` + OnlineUserCleanEvent |
| `PUT /system/role/permission` | `system:role:edit` `@Log(UPDATE)` `@RepeatSubmit` | JSON `SysRoleBo` — **menuIds + deptIds + dataScope** (menu & data permission) | `R<Void>` + OnlineUserCleanEvent |
| `PUT /system/role/changeStatus` | `system:role:edit` `@Log(UPDATE)` `@RepeatSubmit` | JSON `SysRoleBo` `{roleId, status}` | `R<Void>` + OnlineUserCleanEvent |
| `DELETE /system/role/{roleIds}` | `system:role:remove` `@Log(DELETE)` | path `Long[]` (comma-joined) | `R<Void>` |
| `GET /system/role/optionselect` | `system:role:query` | query `roleIds[]` (optional) | `R<List<SysRoleVo>>` (status=normal only) |
| `GET /system/role/authUser/allocatedList` | `system:role:list` | query `SysUserBo` incl. `roleId` + PageQuery | `R<PageResult<SysUserVo>>` — users that **have** the role |
| `GET /system/role/authUser/unallocatedList` | `system:role:list` | query `SysUserBo` incl. `roleId` + PageQuery | `R<PageResult<SysUserVo>>` — users that do **not** have the role |
| `PUT /system/role/authUser/cancel` | `system:role:edit` `@Log(GRANT)` `@RepeatSubmit` | JSON body `SysUserRole` `{userId, roleId}` | `R<Void>` |
| `PUT /system/role/authUser/cancelAll` | `system:role:edit` `@Log(GRANT)` `@RepeatSubmit` | **query params** `roleId` + `userIds[]` | `R<Void>` |
| `PUT /system/role/authUser/selectAll` | `system:role:edit` `@Log(GRANT)` `@RepeatSubmit` | **query params** `roleId` + `userIds[]` (after `checkRoleDataScope(roleId)`) | `R<Void>` |
| `GET /system/role/deptTree/{roleId}` | `system:role:list` | path roleId | `R<DeptTreeSelectVo>` `{checkedKeys: Collection<Long>, depts: List<Tree<Long>>}` |

> **Note vs. classic RuoYi:** there is **no `GET /system/role/authUser/{roleId}`**
> in v6 — it was split into `allocatedList`/`unallocatedList`. `selectAll` /
> `cancelAll` are confirmed (query params, `Long[] userIds`), plus single
> `cancel` (JSON body) and the v6-only `PUT /system/role/permission` split from
> `PUT /system/role`.

Request DTO [SysRoleBo](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/bo/SysRoleBo.java):
`roleId, roleName(@NotBlank ≤30), roleKey(@NotBlank ≤100), roleSort(@NotNull Integer), dataScope, menuCheckStrictly, deptCheckStrictly, status, remark, menuIds(Long[]), deptIds(Long[]), params(Map)`.
Response VO [SysRoleVo](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/domain/vo/SysRoleVo.java):
`roleId, roleName, roleKey, roleSort, dataScope, menuCheckStrictly, deptCheckStrictly, status, remark, createTime, flag(boolean, "user has this role" for authRole views)`.

---

## 4. Business rules

### 4.1 Super-admin protection ([SystemConstants](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-common/ruoyi-common-core/src/main/java/org/dromara/common/core/constant/SystemConstants.java), [SysUserServiceImpl](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysUserServiceImpl.java), [SysRoleServiceImpl](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysRoleServiceImpl.java))

- `SUPER_ADMIN_USER_ID = 1761100000000000001L`, `SUPER_ADMIN_ROLE_ID = 1761300000000000001L`, `SUPER_ADMIN_ROLE_KEY = "superadmin"`.
- `checkUserAllowed(userId)` throws `ServiceException("不允许操作超级管理员用户")` when the target user is the super admin. Called by: user `edit`, `resetPwd`, `changeStatus`, and per-id inside `deleteUserByIds`. → **admin cannot be edited, deactivated, password-reset, or deleted.**
- Controller `remove` additionally rejects deleting the *current logged-in* user ("当前用户不能删除").
- `checkRoleAllowed(role)`:
  - `roleId == SUPER_ADMIN_ROLE_ID` → "不允许操作超级管理员角色" (blocks edit/status/delete).
  - create: `roleKey == "superadmin"` → "不允许使用系统内置管理员角色标识符!".
  - update: if the stored role's `roleKey` is `superadmin`, it cannot be changed; and no other role may be renamed **to** `superadmin`.
- Role deletion also blocks roles with assigned users: `countUserRoleByRoleId(roleId) > 0` → `"<roleName>已分配，不能删除!"`. Role disable (`updateRoleStatus`) likewise: `"角色已分配，不能禁用!"`.
- Super-admin role can never be granted to a normal user: inside `insertUserRole`, `if (!LoginHelper.isSuperAdmin(userId)) roleList.remove(SUPER_ADMIN_ROLE_ID)`; if the list becomes empty → `"不允许为普通用户分配超级管理员角色，请至少选择一个其他角色"`. (`isSuperAdmin(userId)` = `userId == SUPER_ADMIN_USER_ID`.)
- Role-picker responses (`getInfo`, `authRole`) filter out the superadmin role from the *assignable* list unless the target user is the super admin ([LoginHelper.isSuperAdmin](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-common/ruoyi-common-satoken/src/main/java/org/dromara/common/satoken/utils/LoginHelper.java)).

### 4.2 Role-key / name uniqueness

Code-level `exists()` checks excluding self (`neIfPresent(roleId)`) — `checkRoleNameUnique` / `checkRoleKeyUnique` in [SysRoleServiceImpl](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-modules/ruoyi-system/src/main/java/org/dromara/system/service/impl/SysRoleServiceImpl.java). **No DB unique constraints** (confirmed in DDL). Because MyBatis-Plus `@TableLogic` appends `del_flag='0'` to every generated query, soft-deleted rows are excluded → **deleted names/keys become reusable.**

### 4.3 User status transitions & deletion

- `status` `'0'` normal / `'1'` disabled (`SystemConstants.NORMAL`/`DISABLE`). `changeStatus` validates `checkUserAllowed` + `checkUserDataScope`, then `updateUserStatus(userId, status)` — a plain `SET status = ?` update; no additional transition matrix in code.
- Delete = soft delete (`del_flag='1'` via `@TableLogic`). `deleteUserByIds` first deletes `sys_user_role` and `sys_user_post` rows (hard delete) for those ids, then `userMapper.deleteByIds` (soft). All inside one transaction (`@Transactional(rollbackFor = Exception.class)`).
- `checkUserDataScope(userId)`: skips when null or when the caller is super admin; otherwise `countUserById(userId)` (data-permission-filtered count) must be ≥ 1 or `"没有权限访问用户数据！"` is thrown.

### 4.4 Data scope (`data_scope`) semantics ([DataScopeType.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-common/ruoyi-common-mybatis/src/main/java/org/dromara/common/mybatis/enums/DataScopeType.java), [数据权限 docs](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions.md))

Role → **single** `data_scope` value; SQL is injected by the `PlusDataPermissionInterceptor` into any Mapper method annotated `@DataPermission(@DataColumn(key=..., value=...))`:

| code | name | SQL template (SpEL) |
|---|---|---|
| `1` | ALL 全部数据权限 | (no condition) |
| `2` | CUSTOM 自定数据权限 | `#{#deptName} IN ( @sdss.getRoleCustom(roleId) )` — dept ids from `sys_role_dept` |
| `3` | DEPT 本部门数据权限 | `#{#deptName} = #{#user.deptId}` |
| `4` | DEPT_AND_CHILD 本部门及以下 | `#{#deptName} IN ( @sdss.getDeptAndChild(user.deptId) )` |
| `5` | SELF 仅本人 | `#{#userName} = #{#user.userId}` — `userName` key maps to the annotated column, e.g. `create_by` |
| `6` | DEPT_AND_CHILD_OR_SELF 部门及以下或本人 | `IN (getDeptAndChild) OR userName = userId` (v6 addition; classic RuoYi had 1–5) |

- `elseSql` = `1 = 0` (deny) when no matching template. Super admin bypasses data permission entirely. `DataPermissionHelper.ignore(...)` bypasses per-call (used by `getInfo`).
- User mapper annotates `dept_id` → key `deptName`, `create_by` → key `userName`; role mapper annotates `create_dept`/`create_by`. **Data permission applies to queries, updates, and deletes** (not just reads).
- `checkRoleDataScope(roleIds)` uses data-permission-filtered `selectRoleCount(roleIds)`; count must equal requested size or `"没有权限访问部分角色数据！"`.

### 4.5 Password handling

- Hashing: hutool `cn.hutool.crypto.digest.BCrypt` — `BCrypt.hashpw(password)` on **create** (`add`) and **resetPwd** only. (Plus uses Hutool's BCrypt, not Spring Security.)
- **No default password in the backend**; `add` hashes whatever the client sends (frontend form requires it). `resetPwd` body is AES-encrypted (`@ApiEncrypt`).
- **Never serialized**: `SysUserVo.password` is `@JsonIgnore` + `@JsonProperty` (write-only — can be deserialized, never appears in JSON responses); `SystemConstants.EXCLUDE_PROPERTIES = {"password","oldPassword","newPassword","confirmPassword"}` excludes them from logs/param printing.
- `@TableField(..., updateStrategy = NOT_EMPTY)` on `SysUser.password` → user `edit` does **not** touch the password (empty/null is skipped); the edit form never sends it.
- Seed admin hash: `$2a$10$7JB720yubVSZvUI0rEqK/.VqGOZTH.ulu33dHOiBE8ByOhJIrdAu2` (standard RuoYi admin password).

### 4.6 Uniqueness checks (user)

- `checkUserNameUnique` — always enforced on add/edit (login name unique among non-deleted rows).
- `checkPhoneUnique` / `checkEmailUnique` — enforced **only when the field is non-empty** (`StringUtils.isNotEmpty(...)` guard in controller).
- All three are code-level `exists()` with `neIfPresent(userId)` — **no DB unique constraints**; soft-deleted rows don't block reuse.

### 4.7 Cache / side effects

- `@CacheEvict` on user update/delete (`sys:nickname`), role permission update (`sys:role:custom`), role delete (all entries).
- `OnlineUserCleanEvent` published on role base-edit, role permission change, role status change, and user↔role grant/cancel — **kicks online users** whose roles changed (their cached permissions are evicted and sessions logged out via Sa-Token `StpUtil.logoutByTokenValue`).

---

## 5. User ↔ role binding semantics

- **Many-to-many** through `sys_user_role` (composite PK `(user_id, role_id)`; extra index on `role_id`). A user has many roles (`SysUserBo.roleIds: Long[]`); a role has many users.
- **Assign roles to a user:** `PUT /system/user/authRole?userId=1&roleIds=2,3` (arrays as query params) → `insertUserAuth` → `insertUserRole(userId, roleIds, clear=true)`: deletes all existing rows for the user, then batch-inserts. Guards: super-admin role strip (§4.1), `selectRoleCount` data-permission check (`"没有权限访问角色的数据"`), `checkUserDataScope(userId)`.
  - User create/edit (`POST`/`PUT /system/user`) also bind roles via `insertUserRole(user, clear)`; **empty `roleIds` → early return, existing bindings untouched** (edit never clears when roleIds absent).
- **Roles of a user:** `GET /system/user/authRole/{userId}` returns the full assignable role list with `flag=true` on assigned ones (`selectRolesAuthByUserId`); `GET /system/user/{userId}` returns `roleIds: List<Long>` (`selectRoleListByUserId`).
- **Users of a role:** `GET /system/role/authUser/allocatedList` (JOIN `sys_user_role` + `sys_role` filtered by `roleId`), `unallocatedList` (computed as all users minus `selectUserIdsByRoleId(roleId)`).
- **Select/cancel users for a role:**
  - `PUT /system/role/authUser/selectAll?roleId=1&userIds=2,3` → `insertAuthUsers` — batch-inserts bindings (no dedup/clear; PK collision would fail insert). Controller guards `checkRoleDataScope(roleId)`.
  - `PUT /system/role/authUser/cancelAll?roleId=1&userIds=2,3` → `deleteAuthUsers` — batch deletes by `(roleId, userIds IN ...)`.
  - `PUT /system/role/authUser/cancel` body `{userId, roleId}` → `deleteAuthUser` — deletes the single pair.
  - All three throw `"不允许修改当前用户角色!"` when the **current logged-in user** is in the affected user set (you cannot grant/cancel your own role bindings through this API).
  - All publish `OnlineUserCleanEvent.byUsers(...)` to evict those users' cached permissions.
- Role deletion/disable blocked while bound users exist (§4.1); user deletion removes bindings first (§4.3). Bindings are hard-deleted (no `del_flag`).

---

## 6. Layered code structure & conventions

**Stack:** Spring Boot 3 + MyBatis-Plus + MyBatis-Plus-Join (MPJ) + Sa-Token + Hutool + MapStruct (linpeilie auto-mapper). **Mapper XML files are empty** — all SQL is generated by lambda wrappers / MPJ joins.

Layering (per module `ruoyi-modules/ruoyi-system`):

```
Controller (org.dromara.system.controller.system)   HTTP verbs, @SaCheckPermission, validation, toAjax
   └─► ISysUserService / ISysRoleService (service/)  interface
        └─► SysUserServiceImpl / SysRoleServiceImpl (service/impl/)  @Service @Transactional, business rules
             └─► Mappers (mapper/): BaseMapperPlus<Entity, Vo> + MPJBaseMapper<Entity> (joins), @DataPermission annotations
                  └─► sys_user / sys_role / sys_user_role / sys_role_menu / sys_role_dept
```

- **DTO vs entity separation:**
  - `domain/` — entities (DB shape): `SysUser`, `SysRole`, join entities `SysUserRole`, `SysRoleMenu`, `SysRoleDept` (`@TableName`, `@TableId`, `@TableLogic`).
  - `domain/bo/` — request objects (`SysUserBo`, `SysRoleBo`) with Bean-Validation annotations, `@Xss`, `@AutoMapper(target = SysUser.class)`.
  - `domain/vo/` — response objects (`SysUserVo`, `SysRoleVo`, `SysUserInfoVo`, `UserInfoVo`, `SysUserExportVo`, `SysUserImportVo`) with `@Translation`, `@Sensitive`, Excel annotations, `@AutoMapper`.
  - Conversion: `MapstructUtils.convert(bo, Entity.class)` (MapStruct) at service boundary.
- **`BaseEntity` conventions** ([BaseEntity.java](https://github.com/dromara/RuoYi-Vue-Plus/blob/master/ruoyi-common/ruoyi-common-mybatis/src/main/java/org/dromara/common/mybatis/core/domain/BaseEntity.java), [数据库表设计 docs](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/database.md)):
  - `createDept` (`FieldFill.INSERT`), `createBy` (INSERT), `createTime` (INSERT), `updateBy` (INSERT_UPDATE), `updateTime` (INSERT_UPDATE) — auto-filled by a MyBatis-Plus `MetaObjectHandler`; `createBy`/`updateBy` are **Long user IDs**.
  - `del_flag`/`version` are **optional per-entity** (not in BaseEntity): `del_flag` mapped with `@TableLogic` where logic delete is wanted; `version` for optimistic lock.
  - Docs recommend keeping these exact field names for code-generator compatibility.
- **Cross-module API:** the impls also implement `org.dromara.system.api.UserService` / `RoleService` (Dubbo-style/RPC contracts other modules call) and return `UserDTO`/`RoleDTO` — the intra-system DTOs.
- **Pagination:** `PageQuery{pageNum,pageSize,orderByColumn,isAsc}` → `pageQuery.build()` → MP `Page`; results wrapped in `PageResult{total, rows}`.
- **Query convention:** `QueryBuilder.lambda(Entity.class)` / `QueryBuilder.lambdaJoin("u", Entity.class)` with helpers `eqIfPresent`, `likeIfText`, `betweenParams`, `inIfNotEmpty`, `orderByAsc`; join queries use `leftJoin(...)` + `selectJoinList/selectJoinPage`.
- **Data permission** is declared per Mapper method via `@DataPermission({@DataColumn(key="deptName", value="dept_id"), @DataColumn(key="userName", value="create_by")})` (alias-aware for joins) — see [数据权限 docs](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions.md).

---

## Sources

- Code (all verified byte-identical to `master`, 2026-08): GitHub `dromara/RuoYi-Vue-Plus` — links inline above; repo: https://github.com/dromara/RuoYi-Vue-Plus (Gitee mirror: https://gitee.com/dromara/RuoYi-Vue-Plus).
- SQL schemas: https://github.com/dromara/RuoYi-Vue-Plus/blob/master/script/sql/ry_vue.sql (MySQL), `script/sql/postgres/postgres_ry_vue.sql`, `script/sql/oracle/oracle_ry_vue.sql`, `script/sql/sqlserver/sqlserver_ry_vue.sql`.
- Official docs (plus-doc.dromara.org, 6.X): [数据库表设计](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/database.md) · [系统用户相关](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/user.md) · [权限控制](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions_control.md) · [数据权限](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/basic/permissions.md) · [主键使用说明](https://plus-doc.dromara.org/#/6.X/ruoyi-vue-plus/framework/explain/key.md).
