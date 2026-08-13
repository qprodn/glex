-- sys_user — system users (系统用户), ported from RuoYi sys_user.
-- Hand-written record of the production schema; keep aligned with
-- internal/data/ent/schema/user.go (no auto drift guard — discipline, per ADR).
-- PostgreSQL (ADR-0003). Single-tenant (ADR-0001). Snowflake PK (R3-Q1).
-- Soft delete via deleted_at (R2-Q3). Dev also runs ent auto-migrate.

create table if not exists sys_user (
    user_id      bigint       not null,
    user_name    varchar(256) not null,
    nick_name    varchar(256) not null default '',
    email        varchar(256),                       -- nullable; unique when set
    phone_number varchar(256),                       -- nullable; unique when set
    gender       smallint     not null default 0,    -- 0男 1女 2未知
    status       smallint     not null default 0,    -- 0正常 1停用
    password     varchar(256) not null,              -- bcrypt hash
    avatar       varchar(256),
    remark       varchar(256),
    create_by    bigint       not null default 0,    -- audit; auth module fills
    update_by    bigint       not null default 0,
    login_ip     varchar(256),                       -- auth module writes
    login_date   timestamptz,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,                        -- soft delete; NULL = active
    constraint sys_user_pkey primary key (user_id),
    constraint sys_user_user_name_key unique (user_name),
    constraint sys_user_email_key unique (email),
    constraint sys_user_phone_number_key unique (phone_number)
);

comment on table sys_user is '系统用户';

-- sys_menu — menu/permission tree (菜单权限表), ported from RuoYi sys_menu.
-- Hand-written record of the production schema; keep aligned with
-- internal/data/ent/schema/menu.go (no auto drift guard — discipline, per ADR).
-- PostgreSQL (ADR-0003). Single-tenant (ADR-0001). Snowflake PK. Soft delete
-- via deleted_at (RuoYi hard-deletes menus; glex soft-deletes for consistency).
-- char(1) enums → smallint; strings varchar(256) (uniform with sys_user).

create table if not exists sys_menu (
    menu_id     bigint       not null,
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
    create_by   bigint       not null default 0,    -- audit; auth module fills
    update_by   bigint       not null default 0,
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now(),
    deleted_at  timestamptz,                        -- soft delete; NULL = active
    constraint sys_menu_pkey primary key (menu_id)
);
-- No unique constraint on (parent_id, menu_name): enforced by a soft biz check.

comment on table sys_menu is '菜单权限表';
