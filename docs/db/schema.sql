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
