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


create table if not exists sys_file (
    id            bigint       not null,
    original_name varchar(256) not null,               -- original file name as uploaded
    suffix        varchar(16)  not null default '',    -- lowercased extension without the dot ("" when none); ≤16 chars by the domain invariant
    mime_type     varchar(128) not null,               -- content type sniffed from the bytes; client declarations never reach it
    size_bytes    bigint       not null,               -- byte size
    bucket        varchar(128) not null,               -- object storage bucket (single static config, ADR 0007)
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
-- yield two Stored Files (CONTEXT.md "Content Fingerprint").
create index if not exists file_content_hash
    on sys_file (content_hash);

comment on table sys_file is '存储文件表（RuoYi sys_oss 的改名延续，见 CONTEXT.md）';

comment on column sys_file.id is '主键ID（雪花；同时出现在 Stored Key 中）';
comment on column sys_file.original_name is '原始文件名';
comment on column sys_file.suffix is '小写扩展名（不含点；无扩展名为空串）';
comment on column sys_file.mime_type is '内容类型（按字节嗅探；客户端声明不生效）';
comment on column sys_file.size_bytes is '字节大小';
comment on column sys_file.bucket is '对象存储桶（单一静态配置，ADR 0007）';
comment on column sys_file.obj_key is '存储键：[<biz>/]yyyy/MM/dd/<雪花>.<扩展名>；无公开 URL（ADR 0008）';
comment on column sys_file.content_hash is '内容指纹（sha256 hex）；仅完整性记录，不做去重';
comment on column sys_file.create_by is '创建者用户ID';
comment on column sys_file.update_by is '更新者用户ID';
comment on column sys_file.created_at is '创建时间';
comment on column sys_file.updated_at is '更新时间';
comment on column sys_file.deleted_at is '软删除时间；NULL=未删除；撤回同时删除字节（ADR 0009）';
