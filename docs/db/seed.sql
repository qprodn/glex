-- ============================================================================
-- glex 初始化数据（seed）
--
-- 前置：先执行同目录 schema.sql 建表，再执行本文件。
-- 幂等：所有 insert 均带 on conflict do nothing，可重复执行。
-- 审计：create_by / update_by = 0 表示系统自身写入（ADR 0002）。
-- ID：固定种子 ID 取 1e18 区间，低于 bwmarrin/snowflake（默认 epoch、
--      node 1）当前产生的 ID 下限（约 2.1e18），不会与运行期新 ID 冲突。
-- 账号：admin / admin123；user / 123456（bcrypt，DefaultCost = 10）。
-- ============================================================================

begin;

-- ---------------------------------------------------------------------------
-- 用户
-- email / phone 留 NULL（schema 语义：NULL = 未设置）；
-- login_ip / login_date 由认证模块写入，这里不初始化。
-- ---------------------------------------------------------------------------

insert into sys_user (id, user_name, nick_name, gender, status, password, remark, create_by, update_by)
values (1000000000000000001, 'admin', '超级管理员', 2, 0,
        '$2a$10$knIdlVBD86y2IsLpfCkaOeOkEYvqQwLUriRMmspWKV1LtqdrBq602',
        '内置超级管理员，经 superadmin 角色解析出 *:*:* 通配权限', 0, 0)
on conflict do nothing;

insert into sys_user (id, user_name, nick_name, gender, status, password, remark, create_by, update_by)
values (1000000000000000002, 'user', '演示用户', 2, 0,
        '$2a$10$y9LUf.cn2MxtHhGO8ppUL.DNR4Ebwge8HTCwQ6rriX7VHKCdRQlSW',
        '只读演示账号，绑定 common 角色', 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 角色
-- role_key = 'superadmin' 在授权解析中被特殊处理（CONTEXT.md "Superadmin"），
-- 不可改名；menu_check_strictly 仅影响角色编辑器的菜单树展示。
-- ---------------------------------------------------------------------------

insert into sys_role (id, role_name, role_key, role_sort, status, menu_check_strictly, remark, create_by, update_by)
values (1000000000000000101, '超级管理员', 'superadmin', 1, 0, true,
        '拥有全部权限，授权解析为 *:*:*；role_key 由代码特殊识别，勿改', 0, 0),
       (1000000000000000102, '普通角色', 'common', 2, 0, true,
        '演示角色：仅各模块只读权限', 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 菜单
-- menu_type：1 目录 / 2 菜单 / 3 按钮；visible 默认 0 显示，status 默认 0 正常，
-- is_frame 默认 0 否，is_cache 默认 1 缓存，均走列默认值。
-- perms 与 internal 各服务 OperationSelector 声明的权限串一一对应：
-- system:user / system:role / system:menu 各 list、query、add、edit、remove。
-- ---------------------------------------------------------------------------

-- 系统管理（目录）
insert into sys_menu (id, menu_name, parent_id, order_num, path, menu_type, icon, create_by, update_by)
values (1000000000000000201, '系统管理', 0, 1, 'system', 1, 'system', 0, 0)
on conflict do nothing;

-- 用户管理（菜单 + 按钮）
insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000210, '用户管理', 1000000000000000201, 1, 'user', 'iam/user/index', 2, 'system:user:list', 'user', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000211, '用户查询', 1000000000000000210, 1, 3, 'system:user:query', 0, 0),
       (1000000000000000212, '用户新增', 1000000000000000210, 2, 3, 'system:user:add', 0, 0),
       (1000000000000000213, '用户修改', 1000000000000000210, 3, 3, 'system:user:edit', 0, 0),
       (1000000000000000214, '用户删除', 1000000000000000210, 4, 3, 'system:user:remove', 0, 0)
on conflict do nothing;

-- 角色管理（菜单 + 按钮）
insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000220, '角色管理', 1000000000000000201, 2, 'role', 'iam/role/index', 2, 'system:role:list', 'peoples', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000221, '角色查询', 1000000000000000220, 1, 3, 'system:role:query', 0, 0),
       (1000000000000000222, '角色新增', 1000000000000000220, 2, 3, 'system:role:add', 0, 0),
       (1000000000000000223, '角色修改', 1000000000000000220, 3, 3, 'system:role:edit', 0, 0),
       (1000000000000000224, '角色删除', 1000000000000000220, 4, 3, 'system:role:remove', 0, 0)
on conflict do nothing;

-- 菜单管理（菜单 + 按钮）
insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000230, '菜单管理', 1000000000000000201, 3, 'menu', 'iam/menu/index', 2, 'system:menu:list', 'tree-table', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000231, '菜单查询', 1000000000000000230, 1, 3, 'system:menu:query', 0, 0),
       (1000000000000000232, '菜单新增', 1000000000000000230, 2, 3, 'system:menu:add', 0, 0),
       (1000000000000000233, '菜单修改', 1000000000000000230, 3, 3, 'system:menu:edit', 0, 0),
       (1000000000000000234, '菜单删除', 1000000000000000230, 4, 3, 'system:menu:remove', 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 角色菜单授权（sys_role_menu）
-- superadmin：全部菜单；common：目录 + 页面（含 list）+ 各查询按钮。
-- ---------------------------------------------------------------------------

insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000301, 1000000000000000101, 1000000000000000201, 0, 0),
       (1000000000000000302, 1000000000000000101, 1000000000000000210, 0, 0),
       (1000000000000000303, 1000000000000000101, 1000000000000000211, 0, 0),
       (1000000000000000304, 1000000000000000101, 1000000000000000212, 0, 0),
       (1000000000000000305, 1000000000000000101, 1000000000000000213, 0, 0),
       (1000000000000000306, 1000000000000000101, 1000000000000000214, 0, 0),
       (1000000000000000307, 1000000000000000101, 1000000000000000220, 0, 0),
       (1000000000000000308, 1000000000000000101, 1000000000000000221, 0, 0),
       (1000000000000000309, 1000000000000000101, 1000000000000000222, 0, 0),
       (1000000000000000310, 1000000000000000101, 1000000000000000223, 0, 0),
       (1000000000000000311, 1000000000000000101, 1000000000000000224, 0, 0),
       (1000000000000000312, 1000000000000000101, 1000000000000000230, 0, 0),
       (1000000000000000313, 1000000000000000101, 1000000000000000231, 0, 0),
       (1000000000000000314, 1000000000000000101, 1000000000000000232, 0, 0),
       (1000000000000000315, 1000000000000000101, 1000000000000000233, 0, 0),
       (1000000000000000316, 1000000000000000101, 1000000000000000234, 0, 0)
on conflict do nothing;

insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000401, 1000000000000000102, 1000000000000000201, 0, 0),
       (1000000000000000402, 1000000000000000102, 1000000000000000210, 0, 0),
       (1000000000000000403, 1000000000000000102, 1000000000000000211, 0, 0),
       (1000000000000000404, 1000000000000000102, 1000000000000000220, 0, 0),
       (1000000000000000405, 1000000000000000102, 1000000000000000221, 0, 0),
       (1000000000000000406, 1000000000000000102, 1000000000000000230, 0, 0),
       (1000000000000000407, 1000000000000000102, 1000000000000000231, 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 用户角色分配（sys_user_role）
-- ---------------------------------------------------------------------------

insert into sys_user_role (id, user_id, role_id, create_by, update_by)
values (1000000000000000501, 1000000000000000001, 1000000000000000101, 0, 0),
       (1000000000000000502, 1000000000000000002, 1000000000000000102, 0, 0)
on conflict do nothing;

commit;
