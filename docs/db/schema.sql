-- sys_user — system users (系统用户), ported from RuoYi sys_user.
-- Hand-written record of the production schema; keep aligned with
-- internal/data/ent/schema/user.go (no auto drift guard — discipline, per ADR).
-- PostgreSQL (ADR-0003). Single-tenant (ADR-0001). Snowflake PK (R3-Q1).
-- Soft delete via deleted_at (R2-Q3). Dev also runs ent auto-migrate.

create table if not exists sys_user (
    user_id      bigint       not null,
    user_name    varchar(256) not null,
    nick_name    varchar(256) not null default '',
    email        varchar(256),                       -- nullable; NULL when unset
    phone_number varchar(256),                       -- nullable; NULL when unset
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
    constraint sys_user_pkey primary key (user_id)
);
-- No unique constraints on user_name/email/phone_number: enforced by code
-- checks against active rows; soft-deleted values are reusable (repo-wide
-- convention, see .scratch/role/plan.md).

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

-- sys_role — roles (角色信息表), ported from RuoYi sys_role.
-- Hand-written record of the production schema; keep aligned with
-- internal/data/ent/schema/role.go (no auto drift guard — discipline, per ADR).
-- PostgreSQL (ADR-0003). Single-tenant (ADR-0001). Snowflake PK. Soft delete
-- via deleted_at (replaces RuoYi del_flag). No unique constraints — enforced
-- by code checks against active rows; soft-deleted values are reusable
-- (repo-wide convention, see .scratch/role/plan.md).

create table if not exists sys_role (
    role_id            bigint       not null,
    role_name          varchar(256) not null,
    role_key           varchar(256) not null,               -- permission string (no charset rule)
    role_sort          integer      not null default 0,     -- display order
    status             smallint     not null default 0,     -- 0正常 1停用
    menu_check_strictly boolean      not null default true, -- 菜单树选择项是否关联显示 (RuoYi default 1)
    remark             varchar(256) not null default '',
    create_by          bigint       not null default 0,     -- audit; auth module fills
    update_by          bigint       not null default 0,
    created_at         timestamptz  not null default now(),
    updated_at         timestamptz  not null default now(),
    deleted_at         timestamptz,                         -- soft delete; NULL = active
    constraint sys_role_pkey primary key (role_id)
);

comment on table sys_role is '角色信息表';

-- sys_role_menu — role↔menu Menu Grant (角色和菜单关联表), ported from RuoYi.
-- Generated by ent as the M2M join table of Role.menus / Menu.roles (see
-- internal/data/ent/schema/{role,menu}.go); keep aligned by hand.
-- Exactly two columns + composite PK, like RuoYi. No soft delete: rows are
-- hard-deleted on full replacement (UpdateRoleMenus), role delete, and menu
-- delete.

create table if not exists sys_role_menu (
    role_id bigint not null,
    menu_id bigint not null,
    constraint sys_role_menu_pkey primary key (role_id, menu_id)
);

comment on table sys_role_menu is '角色和菜单关联表';
