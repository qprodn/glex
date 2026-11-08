create table if not exists sys_user (
    id           bigint       not null,
    user_name    varchar(256) not null,
    nick_name    varchar(256) not null default '',
    email        varchar(256),                       -- nullable; NULL when unset
    phone_number varchar(256),                       -- nullable; NULL when unset
    gender       smallint     not null default 0,    -- 0男 1女 2未知
    status       smallint     not null default 0,    -- 0正常 1停用
    password     varchar(256) not null,              -- bcrypt hash
    avatar       varchar(256),
    remark       varchar(256),
    dept_id      bigint       not null default 0,    -- org structure reference (ADR 0013); 0 = none
    create_by    bigint       not null default 0,    -- audit; Application stamps from Principal (ADR 0002)
    update_by    bigint       not null default 0,
    login_ip     varchar(256),                       -- auth module writes
    login_date   timestamptz,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,                        -- soft delete; NULL = active
    constraint sys_user_pkey primary key (id)
);

comment on table sys_user is '系统用户';

comment on column sys_user.id is '用户ID';
comment on column sys_user.user_name is '用户账号';
comment on column sys_user.nick_name is '用户昵称';
comment on column sys_user.email is '用户邮箱；NULL 表示未设置';
comment on column sys_user.phone_number is '手机号码；NULL 表示未设置';
comment on column sys_user.gender is '性别：0男 1女 2未知';
comment on column sys_user.status is '账号状态：0正常 1停用';
comment on column sys_user.password is '密码（bcrypt 哈希）';
comment on column sys_user.avatar is '头像地址';
comment on column sys_user.remark is '备注';
comment on column sys_user.dept_id is '部门ID（ADR 0013）；0=未设置；org 模块经 crossdomain 契约校验';
comment on column sys_user.create_by is '创建者用户ID';
comment on column sys_user.update_by is '更新者用户ID';
comment on column sys_user.login_ip is '最后登录IP；认证模块写入，其余模块只读';
comment on column sys_user.login_date is '最后登录时间；认证模块写入，其余模块只读';
comment on column sys_user.created_at is '创建时间';
comment on column sys_user.updated_at is '更新时间';
comment on column sys_user.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_menu (
    id          bigint       not null,
    menu_name   varchar(256) not null,
    parent_id   bigint       not null default 0,    -- 0 = root
    order_num   integer      not null default 0,
    path        varchar(256) not null default '',   -- route address (inert until auth)
    component   varchar(256) not null default '',   -- vue component path
    query_param varchar(256) not null default '',   -- route query (json object)
    is_frame    smallint     not null default 0,    -- 0否 1是 (external link)
    is_cache    smallint     not null default 1,    -- 0不缓存 1缓存
    menu_type   smallint     not null,              -- 1目录 2菜单 3按钮 (MenuType enum)
    visible     smallint     not null default 0,    -- 0显示 1隐藏
    status      smallint     not null default 0,    -- 0正常 1停用
    perms       varchar(256) not null default '',   -- permission handle; enforced by auth
    icon        varchar(256) not null default '',
    active_menu varchar(256) not null default '',
    ext         varchar(256) not null default '',   -- extension (RuoYi 2000; glex 256)
    create_by   bigint       not null default 0,    -- audit; Application stamps from Principal (ADR 0002)
    update_by   bigint       not null default 0,
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now(),
    deleted_at  timestamptz,                        -- soft delete; NULL = active
    constraint sys_menu_pkey primary key (id)
);
-- No unique constraint on (parent_id, menu_name): enforced by a soft biz check.

comment on table sys_menu is '菜单权限表';

comment on column sys_menu.id is '菜单ID';
comment on column sys_menu.menu_name is '菜单名称；同级唯一性由代码层校验';
comment on column sys_menu.parent_id is '父菜单ID；0=根节点（自引用）';
comment on column sys_menu.order_num is '显示顺序';
comment on column sys_menu.path is '路由地址；认证模块 getRouters 消费';
comment on column sys_menu.component is '组件路径（vue）';
comment on column sys_menu.query_param is '路由参数（json 对象）';
comment on column sys_menu.is_frame is '是否为外链：0否 1是';
comment on column sys_menu.is_cache is '是否缓存：0不缓存 1缓存';
comment on column sys_menu.menu_type is '菜单类型（MenuType 枚举）：1目录 2菜单 3按钮';
comment on column sys_menu.visible is '显示状态：0显示 1隐藏';
comment on column sys_menu.status is '菜单状态：0正常 1停用';
comment on column sys_menu.perms is '权限标识；由鉴权模块校验';
comment on column sys_menu.icon is '菜单图标';
comment on column sys_menu.active_menu is '激活菜单（高亮的路由地址）';
comment on column sys_menu.ext is '扩展字段';
comment on column sys_menu.create_by is '创建者用户ID';
comment on column sys_menu.update_by is '更新者用户ID';
comment on column sys_menu.created_at is '创建时间';
comment on column sys_menu.updated_at is '更新时间';
comment on column sys_menu.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_role (
    id                 bigint       not null,
    role_name          varchar(256) not null,
    role_key           varchar(256) not null,               -- permission string (no charset rule)
    role_sort          integer      not null default 0,     -- display order
    status             smallint     not null default 0,     -- 0正常 1停用
    menu_check_strictly boolean      not null default true, -- 菜单树选择项是否关联显示 (RuoYi default 1)
    remark             varchar(256) not null default '',
    create_by          bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by          bigint       not null default 0,
    created_at         timestamptz  not null default now(),
    updated_at         timestamptz  not null default now(),
    deleted_at         timestamptz,                         -- soft delete; NULL = active
    constraint sys_role_pkey primary key (id)
);

comment on table sys_role is '角色信息表';

comment on column sys_role.id is '角色ID';
comment on column sys_role.role_name is '角色名称';
comment on column sys_role.role_key is '权限字符（无字符集规则）';
comment on column sys_role.role_sort is '显示顺序';
comment on column sys_role.status is '角色状态：0正常 1停用';
comment on column sys_role.menu_check_strictly is '菜单树选择项是否关联显示（RuoYi 默认 1）；仅影响角色编辑器的菜单树展示，不影响 sys_role_menu 的存储';
comment on column sys_role.remark is '备注';
comment on column sys_role.create_by is '创建者用户ID';
comment on column sys_role.update_by is '更新者用户ID';
comment on column sys_role.created_at is '创建时间';
comment on column sys_role.updated_at is '更新时间';
comment on column sys_role.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_role_menu (
    id           bigint       not null,
    role_id      bigint       not null,
    menu_id      bigint       not null,
    create_by    bigint       not null default 0,    -- audit; Application stamps from Principal (ADR 0002)
    update_by    bigint       not null default 0,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,                        -- soft delete; NULL = active
    constraint sys_role_menu_pkey primary key (id),
    constraint sys_role_menu_sys_role_menu_grants_fkey foreign key (role_id) references sys_role (id),
    constraint sys_role_menu_sys_menu_role_links_fkey foreign key (menu_id) references sys_menu (id)
);

create unique index if not exists rolemenu_role_id_menu_id
    on sys_role_menu (role_id, menu_id) where deleted_at is null;

comment on table sys_role_menu is '角色和菜单关联表';

comment on column sys_role_menu.id is '主键ID';
comment on column sys_role_menu.role_id is '角色ID';
comment on column sys_role_menu.menu_id is '菜单ID';
comment on column sys_role_menu.create_by is '创建者用户ID';
comment on column sys_role_menu.update_by is '更新者用户ID';
comment on column sys_role_menu.created_at is '创建时间';
comment on column sys_role_menu.updated_at is '更新时间';
comment on column sys_role_menu.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_user_role (
    id           bigint       not null,
    user_id      bigint       not null,
    role_id      bigint       not null,
    create_by    bigint       not null default 0,    -- audit; Application stamps from Principal (ADR 0002)
    update_by    bigint       not null default 0,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,                        -- soft delete; NULL = active
    constraint sys_user_role_pkey primary key (id),
    constraint sys_user_role_sys_user_role_assignments_fkey foreign key (user_id) references sys_user (id),
    constraint sys_user_role_sys_role_user_links_fkey foreign key (role_id) references sys_role (id)
);

create unique index if not exists userrole_user_id_role_id
    on sys_user_role (user_id, role_id) where deleted_at is null;

comment on table sys_user_role is '用户和角色关联表';

comment on column sys_user_role.id is '主键ID';
comment on column sys_user_role.user_id is '用户ID';
comment on column sys_user_role.role_id is '角色ID';
comment on column sys_user_role.create_by is '创建者用户ID';
comment on column sys_user_role.update_by is '更新者用户ID';
comment on column sys_user_role.created_at is '创建时间';
comment on column sys_user_role.updated_at is '更新时间';
comment on column sys_user_role.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_dict_type (
    id         bigint       not null,
    dict_name  varchar(256) not null,
    dict_type  varchar(256) not null,               -- vocabulary key; ^[a-z][a-z0-9_]*$; immutable after create (ADR 0006)
    remark     varchar(256) not null default '',
    create_by  bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by  bigint       not null default 0,
    created_at timestamptz  not null default now(),
    updated_at timestamptz  not null default now(),
    deleted_at timestamptz,                         -- soft delete; NULL = active
    constraint sys_dict_type_pkey primary key (id)
);

-- Unlike sys_role (code-only check), the dict key carries a DB uniqueness
-- guarantee: it is an exact-match identifier every consumer references, and
-- the partial index is the race-proof backstop behind the domain pre-check.
create unique index if not exists dicttype_dict_type
    on sys_dict_type (dict_type) where deleted_at is null;

comment on table sys_dict_type is '字典类型表';

comment on column sys_dict_type.id is '主键ID（雪花）';
comment on column sys_dict_type.dict_name is '字典名称';
comment on column sys_dict_type.dict_type is '字典类型键；满足 ^[a-z][a-z0-9_]*$；创建后不可变（ADR 0006）';
comment on column sys_dict_type.remark is '备注';
comment on column sys_dict_type.create_by is '创建者用户ID';
comment on column sys_dict_type.update_by is '更新者用户ID';
comment on column sys_dict_type.created_at is '创建时间';
comment on column sys_dict_type.updated_at is '更新时间';
comment on column sys_dict_type.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_dict_data (
    id         bigint       not null,
    dict_sort  integer      not null default 0,     -- display order
    dict_label varchar(256) not null,
    dict_value varchar(256) not null,
    dict_type  varchar(256) not null,               -- owning type key; immutable after create (ADR 0006); no FK (RuoYi style)
    list_class varchar(256) not null default 'default', -- tag color preset: default/primary/success/warning/danger/info (proto tag_style)
    is_default boolean      not null default false, -- forms preselect; multiple defaults may coexist (RuoYi semantics)
    remark     varchar(256) not null default '',
    create_by  bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by  bigint       not null default 0,
    created_at timestamptz  not null default now(),
    updated_at timestamptz  not null default now(),
    deleted_at timestamptz,                         -- soft delete; NULL = active
    constraint sys_dict_data_pkey primary key (id)
);

create unique index if not exists dictdata_dict_type_dict_value
    on sys_dict_data (dict_type, dict_value) where deleted_at is null;

comment on table sys_dict_data is '字典数据表（字典项）';

comment on column sys_dict_data.id is '主键ID（雪花；RuoYi 的 dict_code）';
comment on column sys_dict_data.dict_sort is '显示顺序';
comment on column sys_dict_data.dict_label is '字典标签';
comment on column sys_dict_data.dict_value is '字典键值；同类型内唯一（活跃行）';
comment on column sys_dict_data.dict_type is '所属字典类型键；创建后不可变（ADR 0006）；无外键（RuoYi 风格）';
comment on column sys_dict_data.list_class is '标签回显样式（proto tag_style）：default/primary/success/warning/danger/info';
comment on column sys_dict_data.is_default is '是否默认（表单预选）；允许多个默认共存（RuoYi 语义）';
comment on column sys_dict_data.remark is '备注';
comment on column sys_dict_data.create_by is '创建者用户ID';
comment on column sys_dict_data.update_by is '更新者用户ID';
comment on column sys_dict_data.created_at is '创建时间';
comment on column sys_dict_data.updated_at is '更新时间';
comment on column sys_dict_data.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_config (
    id           bigint       not null,
    config_name  varchar(256) not null,               -- display name
    config_key   varchar(256) not null,               -- parameter key; ^[A-Za-z0-9][A-Za-z0-9._-]*$; editable after create (docs/modules/param.md)
    config_value varchar(500) not null default '',    -- plain string; parsing is the consumer's job
    built_in     smallint     not null default 0,     -- built-in parameter: 1 = system assumes it exists, withdrawal refused (RuoYi config_type Y/N normalized)
    remark       varchar(500) not null default '',
    create_by    bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by    bigint       not null default 0,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,                         -- soft delete; NULL = active
    constraint sys_config_pkey primary key (id)
);

-- The key is an exact-match identifier consumer code resolves parameters by,
-- so it carries the same DB uniqueness guarantee the dict key has; the
-- partial index (named as ent's migration creates it) is the race-proof
-- backstop behind the domain pre-check and also serves the crossdomain
-- ConfigReader's by-key point reads.
create unique index if not exists param_config_key
    on sys_config (config_key) where deleted_at is null;

comment on table sys_config is '参数配置表（运行时参数）';

comment on column sys_config.id is '主键ID（雪花）';
comment on column sys_config.config_name is '参数名称';
comment on column sys_config.config_key is '参数键名；满足 ^[A-Za-z0-9][A-Za-z0-9._-]*$；活跃行内唯一；建后可改';
comment on column sys_config.config_value is '参数键值（纯字符串，解析归消费方）';
comment on column sys_config.built_in is '内置参数：1=不可删除；0=普通';
comment on column sys_config.remark is '备注';
comment on column sys_config.create_by is '创建者用户ID';
comment on column sys_config.update_by is '更新者用户ID';
comment on column sys_config.created_at is '创建时间';
comment on column sys_config.updated_at is '更新时间';
comment on column sys_config.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_file (
    id            bigint       not null,
    original_name varchar(256) not null,               -- original file name as uploaded
    suffix        varchar(16)  not null default '',    -- lowercased extension without the dot ("" when none); ≤16 chars by the domain invariant
    mime_type     varchar(128) not null,               -- content type sniffed from the bytes; client declarations never reach it
    size_bytes    bigint       not null,               -- byte size
    bucket        varchar(128) not null,               -- object storage bucket (single static config, ADR 0026)
    obj_key       varchar(512) not null,               -- Stored Key: [<biz>/]yyyy/MM/dd/<snowflake>.<suffix>; no public URL exists (ADR 0008)
    content_hash  varchar(64)  not null,               -- Content Fingerprint: sha256 hex; integrity record, never dedup
    create_by     bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by     bigint       not null default 0,
    created_at    timestamptz  not null default now(),
    updated_at    timestamptz  not null default now(),
    deleted_at    timestamptz,                         -- soft delete; NULL = active; withdrawal also removes the bytes (ADR 0009)
    constraint sys_file_pkey primary key (id)
);

-- Deliberately NOT unique: no dedup exists — identical bytes accepted twice
-- yield two Stored Files (GLOSSARY.md "Content Fingerprint").
create index if not exists file_content_hash
    on sys_file (content_hash);

comment on table sys_file is '存储文件表（RuoYi sys_oss 的改名延续，见 GLOSSARY.md）';

comment on column sys_file.id is '主键ID（雪花；同时出现在 Stored Key 中）';
comment on column sys_file.original_name is '原始文件名';
comment on column sys_file.suffix is '小写扩展名（不含点；无扩展名为空串）';
comment on column sys_file.mime_type is '内容类型（按字节嗅探；客户端声明不生效）';
comment on column sys_file.size_bytes is '字节大小';
comment on column sys_file.bucket is '对象存储桶（单一静态配置，ADR 0026）';
comment on column sys_file.obj_key is '存储键：[<biz>/]yyyy/MM/dd/<雪花>.<扩展名>；无公开 URL（ADR 0008）';
comment on column sys_file.content_hash is '内容指纹（sha256 hex）；仅完整性记录，不做去重';
comment on column sys_file.create_by is '创建者用户ID';
comment on column sys_file.update_by is '更新者用户ID';
comment on column sys_file.created_at is '创建时间';
comment on column sys_file.updated_at is '更新时间';
comment on column sys_file.deleted_at is '软删除时间；NULL=未删除；撤回同时删除字节（ADR 0009）';


create table if not exists sys_oper_log (
    id             bigint       not null,
    operation      varchar(256) not null,               -- kratos operation name, e.g. /iam.v1.UserService/CreateUser
    http_method    varchar(16)  not null default '',
    oper_user_id   bigint       not null default 0,     -- 0 = unauthenticated actor
    oper_user_name varchar(256) not null default '',    -- denormalized; survives account withdrawal
    ipaddr         varchar(256) not null default '',
    request        varchar(2048) not null default '',   -- redacted protojson (secret keys scrubbed, ADR 0012)
    status         smallint     not null default 0,     -- 0成功 1失败
    reason         varchar(256) not null default '',    -- kratos error reason; empty on success
    error_msg      varchar(512) not null default '',
    latency_ms     bigint       not null default 0,
    request_id     varchar(64)  not null default '',    -- correlates with the structured request log
    create_by      bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by      bigint       not null default 0,
    created_at     timestamptz  not null default now(),
    updated_at     timestamptz  not null default now(),
    deleted_at     timestamptz,                         -- soft delete; NULL = active
    constraint sys_oper_log_pkey primary key (id)
);

create index if not exists operlog_created_at on sys_oper_log (created_at);
create index if not exists operlog_oper_user_id on sys_oper_log (oper_user_id);

comment on table sys_oper_log is '操作日志表（RuoYi sys_oper_log）';

comment on column sys_oper_log.id is '主键ID（雪花）';
comment on column sys_oper_log.operation is 'kratos 操作名（如 /iam.v1.UserService/CreateUser）';
comment on column sys_oper_log.http_method is 'HTTP 方法';
comment on column sys_oper_log.oper_user_id is '操作者用户ID；0=未认证操作';
comment on column sys_oper_log.oper_user_name is '操作者账号（冗余；账号撤回后历史可读）';
comment on column sys_oper_log.ipaddr is '操作IP';
comment on column sys_oper_log.request is '请求载荷（脱敏 protojson；ADR 0012）';
comment on column sys_oper_log.status is '操作状态：0成功 1失败';
comment on column sys_oper_log.reason is '错误原因（kratos reason）；成功为空';
comment on column sys_oper_log.error_msg is '错误信息；成功为空';
comment on column sys_oper_log.latency_ms is '耗时（毫秒）';
comment on column sys_oper_log.request_id is '请求ID（与结构化请求日志关联）';
comment on column sys_oper_log.create_by is '创建者用户ID';
comment on column sys_oper_log.update_by is '更新者用户ID';
comment on column sys_oper_log.created_at is '创建时间';
comment on column sys_oper_log.updated_at is '更新时间';
comment on column sys_oper_log.deleted_at is '软删除时间；NULL=未删除；日志只写一次，撤回是唯一变更（ADR 0012）';


create table if not exists sys_login_log (
    id         bigint       not null,
    user_id    bigint       not null default 0,       -- 0 = attempt never resolved to an account
    user_name  varchar(256) not null default '',      -- denormalized; failed attempts still record the name
    ipaddr     varchar(256) not null default '',
    browser    varchar(256) not null default '',
    os         varchar(256) not null default '',
    status     smallint     not null default 0,       -- 0成功 1失败
    msg        varchar(256) not null default '',      -- outcome ("登录成功" / "密码错误" / "强制退出")
    create_by  bigint       not null default 0,       -- the principal that caused the row (ADR 0002); 强退 = the admin
    update_by  bigint       not null default 0,
    created_at timestamptz  not null default now(),
    updated_at timestamptz  not null default now(),
    deleted_at timestamptz,                           -- soft delete; NULL = active
    constraint sys_login_log_pkey primary key (id)
);

create index if not exists loginlog_created_at on sys_login_log (created_at);
create index if not exists loginlog_user_id on sys_login_log (user_id);

comment on table sys_login_log is '登录日志表（RuoYi sys_logininfor 的改名延续）';

comment on column sys_login_log.id is '主键ID（雪花）';
comment on column sys_login_log.user_id is '用户ID；0=尝试未对应到任何账号';
comment on column sys_login_log.user_name is '账号（冗余；失败尝试也记录账号名）';
comment on column sys_login_log.ipaddr is '登录IP';
comment on column sys_login_log.browser is '浏览器（User-Agent 解析）';
comment on column sys_login_log.os is '操作系统（User-Agent 解析）';
comment on column sys_login_log.status is '结果：0成功 1失败';
comment on column sys_login_log.msg is '结果描述（登录成功/密码错误/强制退出等）';
comment on column sys_login_log.create_by is '创建者用户ID；强退记录为执行强退的管理员';
comment on column sys_login_log.update_by is '更新者用户ID';
comment on column sys_login_log.created_at is '创建时间';
comment on column sys_login_log.updated_at is '更新时间';
comment on column sys_login_log.deleted_at is '软删除时间；NULL=未删除；日志只写一次，撤回是唯一变更（ADR 0012）';


create table if not exists sys_dept (
    id         bigint       not null,
    parent_id  bigint       not null default 0,    -- 0 = root; tree derived, no ancestors path (ADR 0014)
    dept_name  varchar(30)  not null,
    order_num  integer      not null default 0,
    leader     bigint       not null default 0,    -- System User id; 0 = none; display-only, never validated
    phone      varchar(11)  not null default '',
    email      varchar(50)  not null default '',
    status     smallint     not null default 0,    -- 0正常 1停用
    create_by  bigint       not null default 0,    -- audit; Application stamps from Principal (ADR 0002)
    update_by  bigint       not null default 0,
    created_at timestamptz  not null default now(),
    updated_at timestamptz  not null default now(),
    deleted_at timestamptz,                        -- soft delete; NULL = active
    constraint sys_dept_pkey primary key (id)
);

create index if not exists dept_parent_id on sys_dept (parent_id);
-- No unique constraint on (parent_id, dept_name): enforced by a soft biz check.

comment on table sys_dept is '部门表';

comment on column sys_dept.id is '部门ID（雪花）';
comment on column sys_dept.parent_id is '父部门ID；0=根部门；树形在领域层内存构建（ADR 0014）';
comment on column sys_dept.dept_name is '部门名称；同级唯一（领域软检查）';
comment on column sys_dept.order_num is '显示顺序';
comment on column sys_dept.leader is '负责人（System User ID）；0=未设置；仅展示，不做存在性校验';
comment on column sys_dept.phone is '联系电话';
comment on column sys_dept.email is '邮箱';
comment on column sys_dept.status is '部门状态：0正常 1停用';
comment on column sys_dept.create_by is '创建者用户ID';
comment on column sys_dept.update_by is '更新者用户ID';
comment on column sys_dept.created_at is '创建时间';
comment on column sys_dept.updated_at is '更新时间';
comment on column sys_dept.deleted_at is '软删除时间；NULL=未删除';


create table if not exists sys_post (
    id         bigint       not null,
    dept_id    bigint       not null,               -- owning department; posts are dept-scoped (ADR 0015)
    post_code  varchar(64)  not null,
    post_name  varchar(50)  not null,
    post_sort  integer      not null default 0,
    status     smallint     not null default 0,     -- 0正常 1停用
    remark     varchar(500) not null default '',
    create_by  bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by  bigint       not null default 0,
    created_at timestamptz  not null default now(),
    updated_at timestamptz  not null default now(),
    deleted_at timestamptz,                         -- soft delete; NULL = active
    constraint sys_post_pkey primary key (id)
);

create index if not exists post_dept_id on sys_post (dept_id);
-- No unique constraints on (dept_id, post_name) / post_code: enforced by soft biz checks.


create table if not exists sys_user_post (
    id         bigint       not null,               -- snowflake surrogate; a re-assigned pair is a new row (ADR 0002)
    user_id    bigint       not null,
    post_id    bigint       not null,
    create_by  bigint       not null default 0,     -- audit; Application stamps from Principal (ADR 0002)
    update_by  bigint       not null default 0,
    created_at timestamptz  not null default now(),
    updated_at timestamptz  not null default now(),
    deleted_at timestamptz,                         -- soft delete; NULL = active
    constraint sys_user_post_pkey primary key (id)
);

create unique index if not exists userpost_user_id_post_id
    on sys_user_post (user_id, post_id)
    where deleted_at is null;

comment on table sys_post is '岗位信息表（部门范围，ADR 0015）';

comment on column sys_post.id is '岗位ID（雪花）';
comment on column sys_post.dept_id is '部门ID；岗位挂靠部门（若依 6.0 形态）';
comment on column sys_post.post_code is '岗位编码；全局唯一（领域软检查）';
comment on column sys_post.post_name is '岗位名称；部门内唯一（领域软检查）';
comment on column sys_post.post_sort is '显示顺序';
comment on column sys_post.status is '岗位状态：0正常 1停用';
comment on column sys_post.remark is '备注';
comment on column sys_post.create_by is '创建者用户ID';
comment on column sys_post.update_by is '更新者用户ID';
comment on column sys_post.created_at is '创建时间';
comment on column sys_post.updated_at is '更新时间';
comment on column sys_post.deleted_at is '软删除时间；NULL=未删除';

comment on table sys_user_post is '用户与岗位关联表（岗位分配）';

comment on column sys_user_post.id is '主键ID（雪花代理键；重新分配同一对是新一行的）';
comment on column sys_user_post.user_id is '用户ID';
comment on column sys_user_post.post_id is '岗位ID';
comment on column sys_user_post.create_by is '创建者用户ID';
comment on column sys_user_post.update_by is '更新者用户ID';
comment on column sys_user_post.created_at is '创建时间';
comment on column sys_user_post.updated_at is '更新时间';
comment on column sys_user_post.deleted_at is '软删除时间；NULL=未删除';

-- ============================================================
-- pay 模块（原生设计，非 RuoYi 血统 —— 不用 sys_ 前缀）
-- ============================================================

create table if not exists pay_merchant_account (
    id             bigint       not null,
    channel        smallint     not null,                  -- 1 微信 2 支付宝 3 云闪付(预留) 4 Stripe(预留)
    name           varchar(64)  not null,                  -- 显示别名；同渠道活跃行内唯一
    status         smallint     not null default 0,        -- 0 启用 1 停用
    config_cipher  text         not null,                  -- 渠道配置 JSON 密文（ADR 0018/0021）
    config_masked  jsonb        not null,                  -- 写入时生成的掩码回显（明文存放，仅掩码）
    config_digest  varchar(64)  not null,                  -- 明文配置 sha256，网关缓存键一半
    key_version    int          not null default 1,        -- 主密钥版本（KEK 轮换预留，ADR 0018）
    remark         varchar(500) not null default '',
    create_by      bigint       not null default 0,
    update_by      bigint       not null default 0,
    created_at     timestamptz  not null default now(),
    updated_at     timestamptz  not null default now(),
    deleted_at     timestamptz,                            -- 软删除；NULL=未删除
    constraint pay_merchant_account_pkey primary key (id)
);

-- 同一渠道的活跃账号展示名唯一；撤除后名字可复用（ADR 0002 软删除）。
create unique index if not exists merchant_channel_name
    on pay_merchant_account (channel, name)
    where deleted_at is null;

comment on table pay_merchant_account is '商户账号表（渠道收款账号，凭据密文落库 ADR 0018/0021）';

comment on column pay_merchant_account.id is '主键ID（雪花）';
comment on column pay_merchant_account.channel is '支付渠道：1微信 2支付宝 3云闪付(预留) 4Stripe(预留)';
comment on column pay_merchant_account.name is '显示别名；同渠道活跃行内唯一（部分唯一索引）';
comment on column pay_merchant_account.status is '账号状态：0启用 1停用（停用仅禁止新支付）';
comment on column pay_merchant_account.config_cipher is '渠道配置 JSON 密文；格式 key_version:nonce_b64:ciphertext_b64';
comment on column pay_merchant_account.config_masked is '掩码回显（如 ****ab12）；展示路径永不解密';
comment on column pay_merchant_account.config_digest is '明文配置 sha256 hex；商户配置重写即变化';
comment on column pay_merchant_account.key_version is '加密所用主密钥版本';
comment on column pay_merchant_account.remark is '备注';
comment on column pay_merchant_account.create_by is '创建者用户ID';
comment on column pay_merchant_account.update_by is '更新者用户ID';
comment on column pay_merchant_account.created_at is '创建时间';
comment on column pay_merchant_account.updated_at is '更新时间';
comment on column pay_merchant_account.deleted_at is '软删除时间；NULL=未删除';

create table if not exists pay_payment (
    id                bigint       not null,             -- 即支付单号，字符串形式作渠道 out_trade_no
    merchant_id       bigint       not null,             -- 收款商户账号（引用，无 FK）
    channel           smallint     not null,             -- 发起时商户渠道快照
    biz_no            varchar(64)  not null,             -- 业务单号（引用，不唯一）
    subject           varchar(256) not null,
    amount            bigint       not null,             -- 金额，最小单位（分），ADR 0019
    currency          char(3)      not null default 'CNY',
    pay_method        smallint     not null default 1,   -- 1 Native 2 JSAPI 3 H5 4 App
    status            smallint     not null default 1,   -- 1 待支付 2 已支付 3 失败 4 已关闭 5 已过期
    channel_trade_no  varchar(64),                       -- 渠道交易号（微信 transaction_id / 支付宝 trade_no）
    channel_payload   text,                              -- 预下单返回（code_url 等），供二维码重渲染
    payer_id          varchar(64),                       -- 付款方（openid / buyer_id）
    refunded_amount   bigint       not null default 0,   -- 已退累计（维护字段，退款推进时累加）
    fail_reason       varchar(256),
    expire_time       timestamptz  not null,             -- 下单时间 + 有效期（默认 30 分钟）
    paid_at           timestamptz,
    create_by         bigint       not null default 0,
    update_by         bigint       not null default 0,
    created_at        timestamptz  not null default now(),
    updated_at        timestamptz  not null default now(),
    deleted_at        timestamptz,                       -- 软删除；NULL=未删除
    constraint pay_payment_pkey primary key (id)
);

-- 惰性过期结算路径与列表过滤的检索索引；均不唯一 —— 重复发起保留历史。
create index if not exists payment_merchant_biz_no
    on pay_payment (merchant_id, biz_no)
    where deleted_at is null;
create index if not exists payment_status
    on pay_payment (status);
create index if not exists payment_created_at
    on pay_payment (created_at);
create index if not exists payment_channel_trade_no
    on pay_payment (channel_trade_no);
-- 过期扫描的候选集：仍 PENDING 且已过期的行（ADR 0023）。
create index if not exists payment_expire_pending
    on pay_payment (expire_time)
    where deleted_at is null and status = 1;

comment on table pay_payment is '支付单表（统一收单；状态推进一律 CAS，终态不可逆）';

comment on column pay_payment.id is '支付单号（雪花）；同时是渠道商户订单号 out_trade_no';
comment on column pay_payment.merchant_id is '收款商户账号ID（pay_merchant_account 引用）';
comment on column pay_payment.channel is '支付渠道（发起时快照）';
comment on column pay_payment.biz_no is '业务单号；调用方引用材料，同一商户可多笔';
comment on column pay_payment.subject is '商品主题';
comment on column pay_payment.amount is '支付金额（最小单位：分），ADR 0019';
comment on column pay_payment.currency is '币种（ISO 4217）；MVP 渠道仅 CNY';
comment on column pay_payment.pay_method is '支付方式：1扫码 2JSAPI 3H5 4App';
comment on column pay_payment.status is '状态：1待支付 2已支付 3失败 4已关闭 5已过期（退款不改状态）';
comment on column pay_payment.channel_trade_no is '渠道交易号';
comment on column pay_payment.channel_payload is '渠道预下单返回原文（code_url 等）';
comment on column pay_payment.payer_id is '付款方标识（openid / buyer_id）';
comment on column pay_payment.refunded_amount is '已退款累计金额（分）；仅退款流程累加';
comment on column pay_payment.fail_reason is '失败/关闭原因';
comment on column pay_payment.expire_time is '过期时间；惰性判定，无定时任务';
comment on column pay_payment.paid_at is '支付完成时间';
comment on column pay_payment.create_by is '创建者用户ID';
comment on column pay_payment.update_by is '更新者用户ID（0=渠道驱动的系统推进）';
comment on column pay_payment.created_at is '创建时间';
comment on column pay_payment.updated_at is '更新时间';
comment on column pay_payment.deleted_at is '软删除时间；NULL=未删除';

create table if not exists pay_refund (
    id                bigint       not null,             -- 即退款单号，作渠道 out_refund_no
    payment_id        bigint       not null,             -- 原支付单（引用，无 FK）
    merchant_id       bigint       not null,
    channel           smallint     not null,             -- 沿支付单渠道
    amount            bigint       not null,             -- 本次退款金额（分），ADR 0019
    reason            varchar(256) not null default '',
    status            smallint     not null default 1,   -- 1 处理中 2 成功 3 失败
    channel_refund_no varchar(64),                       -- 渠道退款单号
    fail_reason       varchar(256),
    refunded_at       timestamptz,
    create_by         bigint       not null default 0,
    update_by         bigint       not null default 0,
    created_at        timestamptz  not null default now(),
    updated_at        timestamptz  not null default now(),
    deleted_at        timestamptz,                       -- 软删除；NULL=未删除
    constraint pay_refund_pkey primary key (id)
);

create index if not exists refund_payment_id
    on pay_refund (payment_id);

comment on table pay_refund is '退款单表（多次部分退款，累计不超过已付金额）';

comment on column pay_refund.id is '退款单号（雪花）；同时是渠道商户退款单号 out_refund_no';
comment on column pay_refund.payment_id is '原支付单ID（pay_payment 引用）';
comment on column pay_refund.merchant_id is '商户账号ID（沿支付单）';
comment on column pay_refund.channel is '支付渠道（沿支付单）';
comment on column pay_refund.amount is '本次退款金额（分），ADR 0019';
comment on column pay_refund.reason is '退款原因';
comment on column pay_refund.status is '状态：1处理中 2成功 3失败（CAS 推进）';
comment on column pay_refund.channel_refund_no is '渠道退款单号';
comment on column pay_refund.fail_reason is '失败原因';
comment on column pay_refund.refunded_at is '退款成功时间';
comment on column pay_refund.create_by is '创建者用户ID';
comment on column pay_refund.update_by is '更新者用户ID（0=渠道驱动的系统推进）';
comment on column pay_refund.created_at is '创建时间';
comment on column pay_refund.updated_at is '更新时间';
comment on column pay_refund.deleted_at is '软删除时间；NULL=未删除';

create table if not exists pay_notify_log (
    id            bigint        not null,
    channel       smallint      not null,
    notify_id     varchar(128)  not null,                 -- 渠道侧通知标识（微信 v3 header id / 支付宝 notify_id）
    merchant_id   bigint        not null,                 -- 路由命中的商户账号
    kind          smallint      not null,                 -- 1 支付 2 退款
    target_no     varchar(64)   not null,                 -- 支付单号或退款单号
    verified      boolean       not null,                 -- 验签是否通过；false 行仅作证据
    processed     boolean       not null,                 -- 幂等命中（重复投递）为 false
    raw_body      text          not null,                 -- 原始请求体（取证锚点）
    sign_headers  jsonb,                                  -- 验签相关请求头
    received_at   timestamptz   not null default now(),
    create_by     bigint        not null default 0,       -- 渠道无 Principal，恒为 0（系统自身）
    update_by     bigint        not null default 0,
    created_at    timestamptz   not null default now(),
    updated_at    timestamptz   not null default now(),
    deleted_at    timestamptz,                            -- 软删除；NULL=未删除
    constraint pay_notify_log_pkey primary key (id)
);

-- (渠道, 通知ID) 活跃行唯一：渠道保证重投，唯一约束即幂等（ADR 0020）。
create unique index if not exists notify_channel_notify_id
    on pay_notify_log (channel, notify_id)
    where deleted_at is null;
create index if not exists notify_merchant_id
    on pay_notify_log (merchant_id);

comment on table pay_notify_log is '渠道回调流水表（write-once 证据流，ADR 0020）';

comment on column pay_notify_log.id is '主键ID（雪花）';
comment on column pay_notify_log.channel is '支付渠道';
comment on column pay_notify_log.notify_id is '渠道通知标识；去重半键';
comment on column pay_notify_log.merchant_id is '路由命中的商户账号ID';
comment on column pay_notify_log.kind is '通知类别：1支付 2退款';
comment on column pay_notify_log.target_no is '通知指向的单号（支付单号/退款单号）';
comment on column pay_notify_log.verified is '验签结果；false 行不推进任何状态';
comment on column pay_notify_log.processed is '是否完成状态推进；幂等命中为 false';
comment on column pay_notify_log.raw_body is '原始请求体（取证）';
comment on column pay_notify_log.sign_headers is '验签相关请求头（取证）';
comment on column pay_notify_log.received_at is '接收时间';
comment on column pay_notify_log.create_by is '创建者用户ID（渠道回调恒为0）';
comment on column pay_notify_log.update_by is '更新者用户ID';
comment on column pay_notify_log.created_at is '创建时间';
comment on column pay_notify_log.updated_at is '更新时间';
comment on column pay_notify_log.deleted_at is '软删除时间；NULL=未删除';

create table if not exists job (
    id              bigint        not null,
    name            varchar(64)   not null,                 -- 展示名，活跃行内唯一
    handler         varchar(128)  not null,                 -- 注册的任务执行器名（ADR 0023，行内不携带可执行文本）
    cron            varchar(64)   not null,                 -- 5 字段 cron 表达式，统一在配置时区求值
    args            text          not null default '',      -- 透传给执行器的参数
    enabled         boolean       not null default true,    -- 暂停/恢复开关
    timeout_seconds integer       not null default 60,      -- 单次运行超时（1–3600）
    remark          varchar(255)  not null default '',
    create_by       bigint        not null default 0,
    update_by       bigint        not null default 0,
    created_at      timestamptz   not null default now(),
    updated_at      timestamptz   not null default now(),
    deleted_at      timestamptz,                            -- 软删除；NULL=未删除
    constraint job_pkey primary key (id)
);

-- 活跃行内名字唯一；撤下后名字可复用（ADR 0002 软删除）。
create unique index if not exists job_name
    on job (name)
    where deleted_at is null;
create index if not exists job_handler
    on job (handler)
    where deleted_at is null;

comment on table job is '定时任务表（原生设计，ADR 0023；无 Quartz、无 invoke_target）';

comment on column job.id is '主键ID（雪花）';
comment on column job.name is '展示名；活跃行内唯一';
comment on column job.handler is '任务执行器注册名；保存时对注册表现场校验';
comment on column job.cron is '5 字段 cron 表达式（robfig ParseStandard 语法）；配置时区统一求值';
comment on column job.args is '执行器参数，原样透传';
comment on column job.enabled is 'true=参与调度；false=暂停（手动立即执行不受限）';
comment on column job.timeout_seconds is '单次运行超时秒数（1–3600），建行时取配置默认值固化';
comment on column job.remark is '备注';
comment on column job.create_by is '创建者用户ID';
comment on column job.update_by is '更新者用户ID';
comment on column job.created_at is '创建时间';
comment on column job.updated_at is '更新时间';
comment on column job.deleted_at is '软删除时间；NULL=未删除';

create table if not exists job_log (
    id           bigint        not null,
    job_id       bigint        not null,                    -- job 引用（无外键；任务撤下后日志仍可读）
    handler      varchar(128)  not null,                    -- 触发时执行器快照
    trigger      smallint      not null,                    -- 1 调度 2 手动 3 外部（预留，ADR 0023）
    status       smallint      not null,                    -- 1 成功 2 失败（无“运行中”——行在完成时才落）
    message      text          not null default '',         -- 运行摘要或错误（截断 2000）
    started_at   timestamptz   not null,
    duration_ms  bigint        not null,
    create_by    bigint        not null default 0,          -- 触发均为系统驱动（含手动），恒为 0
    update_by    bigint        not null default 0,
    created_at   timestamptz   not null default now(),
    updated_at   timestamptz   not null default now(),
    deleted_at   timestamptz,                               -- 软删除；NULL=未删除
    constraint job_log_pkey primary key (id)
);

create index if not exists joblog_job_id_created_at
    on job_log (job_id, created_at)
    where deleted_at is null;
create index if not exists joblog_created_at
    on job_log (created_at);

comment on table job_log is '任务日志表（完成时一次写入，无更新路径，ADR 0023/0012 精神）';

comment on column job_log.id is '主键ID（雪花）';
comment on column job_log.job_id is '所属任务ID（无外键）';
comment on column job_log.handler is '触发时执行器名快照；后续改 handler 不改写历史';
comment on column job_log.trigger is '触发来源：1调度 2手动 3外部（预留）';
comment on column job_log.status is '运行结果：1成功 2失败';
comment on column job_log.message is '运行摘要或错误文本（截断 2000）';
comment on column job_log.started_at is '运行开始时间';
comment on column job_log.duration_ms is '运行时长（毫秒）';
comment on column job_log.create_by is '创建者用户ID（系统驱动恒为0）';
comment on column job_log.update_by is '更新者用户ID';
comment on column job_log.created_at is '创建时间';
comment on column job_log.updated_at is '更新时间';
comment on column job_log.deleted_at is '软删除时间；NULL=未删除';

-- ============================================================
-- wechat 模块（原生设计，非 RuoYi 血统 —— 不用 sys_ 前缀）
-- ============================================================

create table if not exists wechat_platform (
    id                      bigint       not null,
    name                    varchar(64)  not null,             -- 显示名称
    component_app_id        varchar(64)  not null,             -- component AppID；本行存续期间不可改
    secret_cipher           text         not null,             -- component secret / Token / EncodingAESKey 的 JSON 密文（ADR 0018/0029）
    component_secret_mask   varchar(16)  not null,             -- 写入时生成的掩码回显
    token_mask              varchar(16)  not null,
    encoding_aes_key_mask   varchar(16)  not null,
    key_version             int          not null default 1,   -- 主密钥版本（KEK 轮换预留，ADR 0018）
    verify_ticket_cipher    text         not null default '',  -- 当前验证票据密文；空表示未持有。展示路径永不解密、永不回显
    create_by               bigint       not null default 0,
    update_by               bigint       not null default 0,
    created_at              timestamptz  not null default now(),
    updated_at              timestamptz  not null default now(),
    deleted_at              timestamptz,                       -- 软删除；NULL=未删除
    constraint wechat_platform_pkey primary key (id)
);

-- 未撤回的平台里，一个 component AppID 只对应一条；撤回后可以再登记成新行（ADR 0029）。
create unique index if not exists wechat_platform_component_app_id
    on wechat_platform (component_app_id)
    where deleted_at is null;

comment on table wechat_platform is '微信平台表（第三方平台登记，凭据密文落库 ADR 0018/0029）';

comment on column wechat_platform.id is '主键ID（雪花）';
comment on column wechat_platform.name is '显示名称';
comment on column wechat_platform.component_app_id is 'component AppID；本行存续期间不可改；未撤回行内唯一';
comment on column wechat_platform.secret_cipher is 'component secret、Token、EncodingAESKey 的 JSON 密文；格式 key_version:nonce_b64:ciphertext_b64';
comment on column wechat_platform.component_secret_mask is 'component secret 掩码回显（如 ****ab12）；展示路径永不解密';
comment on column wechat_platform.token_mask is 'Token 掩码回显；展示路径永不解密';
comment on column wechat_platform.encoding_aes_key_mask is 'EncodingAESKey 掩码回显；展示路径永不解密';
comment on column wechat_platform.key_version is '加密所用主密钥版本（WECHAT_MASTER_KEY，不是 PAY_MASTER_KEY）';
comment on column wechat_platform.verify_ticket_cipher is '当前验证票据密文；空表示未持有。展示路径永不解密、永不回显明文';
comment on column wechat_platform.create_by is '创建者用户ID';
comment on column wechat_platform.update_by is '更新者用户ID';
comment on column wechat_platform.created_at is '创建时间';
comment on column wechat_platform.updated_at is '更新时间';
comment on column wechat_platform.deleted_at is '软删除时间；NULL=未删除';

create table if not exists wechat_app (
    id                      bigint       not null,
    name                    varchar(64)  not null,             -- 显示名称
    wx_app_id               varchar(64)  not null,             -- 微信 AppID；本行存续期间不可改
    app_type                varchar(32)  not null,             -- mini_program 或 official_account
    account_kind            varchar(32)  not null default '',  -- service_account、subscription_account，小程序为空
    connection              varchar(32)  not null,             -- direct_hold 或 platform_authorization；本行存续期间不可改
    platform_id             bigint       not null default 0,   -- 平台授权所挂的微信平台；直连为 0；本行存续期间不可改
    granted_capabilities    jsonb,                             -- 微信授予的能力名称；直连为空。不是权限集数字编号
    secret_cipher           text         not null,             -- 直连为 AppSecret 与消息推送密钥；平台授权为授权方刷新材料。均为 JSON 密文（ADR 0018/0029）
    app_secret_mask         varchar(16)  not null,             -- 写入时生成的 AppSecret 掩码回显
    token_mask              varchar(16)  not null,
    encoding_aes_key_mask   varchar(16)  not null,
    suspended               boolean      not null default false, -- 停用；不是撤回，也不是失效
    lapsed                  boolean      not null default false, -- 失效；只有微信明确拒绝授权或直连密钥时设置
    key_version             int          not null default 1,   -- 主密钥版本（KEK 轮换预留，ADR 0018）
    create_by               bigint       not null default 0,
    update_by               bigint       not null default 0,
    created_at              timestamptz  not null default now(),
    updated_at              timestamptz  not null default now(),
    deleted_at              timestamptz,                       -- 软删除；NULL=未删除
    constraint wechat_app_pkey primary key (id)
);

-- 未撤回的微信应用里，一个 AppID 只对应一条；与平台 component AppID 的唯一约束互不相关。
-- 撤回后同一个 AppID 可以再接入成新行（ADR 0029）。
create unique index if not exists wechat_app_wx_app_id
    on wechat_app (wx_app_id)
    where deleted_at is null;

create index if not exists wechat_app_platform_id
    on wechat_app (platform_id);

-- 表已按上一版脚本建过时，create table if not exists 不会补列。
alter table wechat_app add column if not exists lapsed boolean not null default false;

comment on table wechat_app is '微信应用表（直连或平台授权，凭据密文落库 ADR 0018/0029）';

comment on column wechat_app.id is '主键ID（雪花）';
comment on column wechat_app.name is '显示名称';
comment on column wechat_app.wx_app_id is '微信 AppID；本行存续期间不可改；未撤回行内唯一，与平台 component AppID 无关';
comment on column wechat_app.app_type is 'mini_program 或 official_account';
comment on column wechat_app.account_kind is '公众号种类：service_account 或 subscription_account；小程序为空';
comment on column wechat_app.connection is '接法：direct_hold 或 platform_authorization；本行存续期间不可改，也不能在两种接法之间改';
comment on column wechat_app.platform_id is '平台授权所挂的微信平台 ID；直连为 0。本行存续期间不可改，也不能换平台';
comment on column wechat_app.granted_capabilities is '微信这次授予的能力名称 JSON 数组；直连为空。不存放权限集数字编号';
comment on column wechat_app.secret_cipher is '直连为 AppSecret、Token、EncodingAESKey；平台授权为授权方刷新材料、不含对方 AppSecret。JSON 密文，格式 key_version:nonce_b64:ciphertext_b64';
comment on column wechat_app.app_secret_mask is 'AppSecret 掩码回显（如 ****ab12）；展示路径永不解密';
comment on column wechat_app.token_mask is '消息推送 Token 掩码回显；展示路径永不解密';
comment on column wechat_app.encoding_aes_key_mask is 'EncodingAESKey 掩码回显；展示路径永不解密';
comment on column wechat_app.suspended is '停用标记；停用不是撤回，也不是失效';
comment on column wechat_app.lapsed is '失效标记；微信明确拒绝授权或直连密钥时设置。超时、业务拒绝、平台未就绪都不设置。换 AppSecret 或在原平台重新授权后清除';
comment on column wechat_app.key_version is '加密所用主密钥版本（WECHAT_MASTER_KEY，不是 PAY_MASTER_KEY）';
comment on column wechat_app.create_by is '创建者用户ID';
comment on column wechat_app.update_by is '更新者用户ID';
comment on column wechat_app.created_at is '创建时间';
comment on column wechat_app.updated_at is '更新时间';
comment on column wechat_app.deleted_at is '软删除时间；NULL=未删除';
