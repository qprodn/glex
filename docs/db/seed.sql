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

-- 字典管理（菜单 + 按钮）；icon 走 @ant-design/icons 运行时解析，book = BookOutlined
insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000240, '字典管理', 1000000000000000201, 6, 'dict', 'iam/dict/index', 2, 'system:dict:list', 'book', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000241, '字典查询', 1000000000000000240, 1, 3, 'system:dict:query', 0, 0),
       (1000000000000000242, '字典新增', 1000000000000000240, 2, 3, 'system:dict:add', 0, 0),
       (1000000000000000243, '字典修改', 1000000000000000240, 3, 3, 'system:dict:edit', 0, 0),
       (1000000000000000244, '字典删除', 1000000000000000240, 4, 3, 'system:dict:remove', 0, 0)
on conflict do nothing;

-- 文件管理（菜单 + 按钮）；perms 与 auth_routes.go 的五个 handle 一一对应
insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000250, '文件管理', 1000000000000000201, 7, 'file', 'file/index', 2, 'system:file:list', 'upload', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000251, '文件查询', 1000000000000000250, 1, 3, 'system:file:query', 0, 0),
       (1000000000000000252, '文件上传', 1000000000000000250, 2, 3, 'system:file:upload', 0, 0),
       (1000000000000000253, '文件下载', 1000000000000000250, 3, 3, 'system:file:download', 0, 0),
       (1000000000000000254, '文件删除', 1000000000000000250, 4, 3, 'system:file:remove', 0, 0)
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
       (1000000000000000407, 1000000000000000102, 1000000000000000231, 0, 0),
       -- 字典管理：页面（含 list）+ 查询按钮
       (1000000000000000408, 1000000000000000102, 1000000000000000240, 0, 0),
       (1000000000000000409, 1000000000000000102, 1000000000000000241, 0, 0),
       -- 文件管理：页面（含 list）+ 查询按钮
       (1000000000000000410, 1000000000000000102, 1000000000000000250, 0, 0),
       (1000000000000000411, 1000000000000000102, 1000000000000000251, 0, 0)
on conflict do nothing;

-- superadmin 追加字典管理全部菜单
insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000317, 1000000000000000101, 1000000000000000240, 0, 0),
       (1000000000000000318, 1000000000000000101, 1000000000000000241, 0, 0),
       (1000000000000000319, 1000000000000000101, 1000000000000000242, 0, 0),
       (1000000000000000320, 1000000000000000101, 1000000000000000243, 0, 0),
       (1000000000000000321, 1000000000000000101, 1000000000000000244, 0, 0)
on conflict do nothing;

-- superadmin 追加文件管理全部菜单
insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000322, 1000000000000000101, 1000000000000000250, 0, 0),
       (1000000000000000323, 1000000000000000101, 1000000000000000251, 0, 0),
       (1000000000000000324, 1000000000000000101, 1000000000000000252, 0, 0),
       (1000000000000000325, 1000000000000000101, 1000000000000000253, 0, 0),
       (1000000000000000326, 1000000000000000101, 1000000000000000254, 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 用户角色分配（sys_user_role）
-- ---------------------------------------------------------------------------

insert into sys_user_role (id, user_id, role_id, create_by, update_by)
values (1000000000000000501, 1000000000000000001, 1000000000000000101, 0, 0),
       (1000000000000000502, 1000000000000000002, 1000000000000000102, 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 监控（目录 + 在线用户/操作日志/登录日志 页面 + 按钮）
-- 在线用户此前只存在于前端（seed 缺失），随本次一并补齐；
-- perms 与 auth_routes.go 的 handle 一一对应；icon 走 @ant-design/icons
-- 运行时解析（desktop/form = DesktopOutlined/FormOutlined）。
-- ---------------------------------------------------------------------------

insert into sys_menu (id, menu_name, parent_id, order_num, path, menu_type, icon, create_by, update_by)
values (1000000000000000260, '监控', 0, 2, 'monitor', 1, 'desktop', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000261, '在线用户', 1000000000000000260, 1, 'online', 'monitor/online/index', 2, 'monitor:online:list', 'online', 0, 0),
       (1000000000000000263, '操作日志', 1000000000000000260, 2, 'operlog', 'monitor/operlog/index', 2, 'monitor:operlog:list', 'form', 0, 0),
       (1000000000000000266, '登录日志', 1000000000000000260, 3, 'loginlog', 'monitor/loginlog/index', 2, 'monitor:loginlog:list', 'log', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000262, '在线强退', 1000000000000000261, 1, 3, 'monitor:online:forceLogout', 0, 0),
       (1000000000000000264, '日志查询', 1000000000000000263, 1, 3, 'monitor:operlog:query', 0, 0),
       (1000000000000000265, '日志删除', 1000000000000000263, 2, 3, 'monitor:operlog:remove', 0, 0),
       (1000000000000000267, '日志删除', 1000000000000000266, 1, 3, 'monitor:loginlog:remove', 0, 0)
on conflict do nothing;

-- superadmin 追加监控全部菜单
insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000327, 1000000000000000101, 1000000000000000260, 0, 0),
       (1000000000000000328, 1000000000000000101, 1000000000000000261, 0, 0),
       (1000000000000000329, 1000000000000000101, 1000000000000000262, 0, 0),
       (1000000000000000330, 1000000000000000101, 1000000000000000263, 0, 0),
       (1000000000000000331, 1000000000000000101, 1000000000000000264, 0, 0),
       (1000000000000000332, 1000000000000000101, 1000000000000000265, 0, 0),
       (1000000000000000333, 1000000000000000101, 1000000000000000266, 0, 0),
       (1000000000000000334, 1000000000000000101, 1000000000000000267, 0, 0)
on conflict do nothing;

-- common：监控目录 + 三个页面（含 list）+ 操作日志查询按钮
insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000412, 1000000000000000102, 1000000000000000260, 0, 0),
       (1000000000000000413, 1000000000000000102, 1000000000000000261, 0, 0),
       (1000000000000000414, 1000000000000000102, 1000000000000000263, 0, 0),
       (1000000000000000415, 1000000000000000102, 1000000000000000264, 0, 0),
       (1000000000000000416, 1000000000000000102, 1000000000000000266, 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 字典类型（sys_dict_type）
-- 前端选项渲染的首批消费方：role/user 状态（sys_normal_disable）与
-- user 性别（sys_user_gender）；键创建后不可变（ADR 0006），勿改。
-- ---------------------------------------------------------------------------

insert into sys_dict_type (id, dict_name, dict_type, remark, create_by, update_by)
values (1000000000000000601, '用户性别', 'sys_user_gender', '用户性别列表', 0, 0),
       (1000000000000000602, '系统启用停用', 'sys_normal_disable', '系统启用停用的通用状态（0正常 1停用）', 0, 0),
       (1000000000000000603, '系统显示隐藏', 'sys_show_hide', '系统显示隐藏的通用状态（0显示 1隐藏）', 0, 0),
       (1000000000000000604, '系统是否', 'sys_yes_no', '系统通用是否（Y是 N否）', 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 字典数据（sys_dict_data，字典项）
-- list_class：default/primary/success/warning/danger/info（proto tag_style）。
-- ---------------------------------------------------------------------------

insert into sys_dict_data (id, dict_sort, dict_label, dict_value, dict_type, list_class, is_default, remark, create_by, update_by)
values (1000000000000000701, 1, '男', '0', 'sys_user_gender', 'default', true, '', 0, 0),
       (1000000000000000702, 2, '女', '1', 'sys_user_gender', 'default', false, '', 0, 0),
       (1000000000000000703, 3, '未知', '2', 'sys_user_gender', 'info', false, '', 0, 0),
       (1000000000000000704, 1, '正常', '0', 'sys_normal_disable', 'primary', true, '', 0, 0),
       (1000000000000000705, 2, '停用', '1', 'sys_normal_disable', 'danger', false, '', 0, 0),
       (1000000000000000706, 1, '显示', '0', 'sys_show_hide', 'primary', true, '', 0, 0),
       (1000000000000000707, 2, '隐藏', '1', 'sys_show_hide', 'info', false, '', 0, 0),
       (1000000000000000708, 1, '是', 'Y', 'sys_yes_no', 'primary', true, '', 0, 0),
       (1000000000000000709, 2, '否', 'N', 'sys_yes_no', 'danger', false, '', 0, 0)
on conflict do nothing;

-- ---------------------------------------------------------------------------
-- 组织结构（ADR 0013/0014/0015）：部门 sys_dept、岗位 sys_post。
-- 根部门 id 固定于 internal/domain/org.RootDeptID（删除守卫锚点）。
-- ---------------------------------------------------------------------------

insert into sys_dept (id, parent_id, dept_name, order_num, leader, phone, email, status, create_by, update_by)
values (1000000000000000801, 0, 'glex科技', 0, 0, '', '', 0, 0, 0),
       (1000000000000000802, 1000000000000000801, '研发部门', 1, 0, '', '', 0, 0, 0),
       (1000000000000000803, 1000000000000000801, '市场部门', 2, 0, '', '', 0, 0, 0)
on conflict do nothing;

insert into sys_post (id, dept_id, post_code, post_name, post_sort, status, remark, create_by, update_by)
values (1000000000000000901, 1000000000000000802, 'fe-dev', '前端开发', 1, 0, '', 0, 0),
       (1000000000000000902, 1000000000000000802, 'be-dev', '后端开发', 2, 0, '', 0, 0),
       (1000000000000000903, 1000000000000000803, 'mkt', '市场专员', 1, 0, '', 0, 0)
on conflict do nothing;

-- 种子用户挂研发部门；幂等：只填空（dept_id=0），不覆盖手工调整。
-- 已有库的字典/文件排序（4/5 → 6/7）同理只对新装库生效。
update sys_user set dept_id = 1000000000000000802, update_by = 0
where id in (1000000000000000001, 1000000000000000002) and dept_id = 0;

-- 部门管理（若依排序：用户1 角色2 菜单3 部门4 岗位5 字典6 文件7）
insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000270, '部门管理', 1000000000000000201, 4, 'dept', 'iam/dept/index', 2, 'system:dept:list', 'apartment', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000271, '部门查询', 1000000000000000270, 1, 3, 'system:dept:query', 0, 0),
       (1000000000000000272, '部门新增', 1000000000000000270, 2, 3, 'system:dept:add', 0, 0),
       (1000000000000000273, '部门修改', 1000000000000000270, 3, 3, 'system:dept:edit', 0, 0),
       (1000000000000000274, '部门删除', 1000000000000000270, 4, 3, 'system:dept:remove', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, path, component, menu_type, perms, icon, create_by, update_by)
values (1000000000000000280, '岗位管理', 1000000000000000201, 5, 'post', 'iam/post/index', 2, 'system:post:list', 'idcard', 0, 0)
on conflict do nothing;

insert into sys_menu (id, menu_name, parent_id, order_num, menu_type, perms, create_by, update_by)
values (1000000000000000281, '岗位查询', 1000000000000000280, 1, 3, 'system:post:query', 0, 0),
       (1000000000000000282, '岗位新增', 1000000000000000280, 2, 3, 'system:post:add', 0, 0),
       (1000000000000000283, '岗位修改', 1000000000000000280, 3, 3, 'system:post:edit', 0, 0),
       (1000000000000000284, '岗位删除', 1000000000000000280, 4, 3, 'system:post:remove', 0, 0)
on conflict do nothing;

-- superadmin 追加部门/岗位管理全部菜单
insert into sys_role_menu (id, role_id, menu_id, create_by, update_by)
values (1000000000000000327, 1000000000000000101, 1000000000000000270, 0, 0),
       (1000000000000000328, 1000000000000000101, 1000000000000000271, 0, 0),
       (1000000000000000329, 1000000000000000101, 1000000000000000272, 0, 0),
       (1000000000000000330, 1000000000000000101, 1000000000000000273, 0, 0),
       (1000000000000000331, 1000000000000000101, 1000000000000000274, 0, 0),
       (1000000000000000332, 1000000000000000101, 1000000000000000280, 0, 0),
       (1000000000000000333, 1000000000000000101, 1000000000000000281, 0, 0),
       (1000000000000000334, 1000000000000000101, 1000000000000000282, 0, 0),
       (1000000000000000335, 1000000000000000101, 1000000000000000283, 0, 0),
       (1000000000000000336, 1000000000000000101, 1000000000000000284, 0, 0)
on conflict do nothing;

commit;
