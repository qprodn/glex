# param 模块指南：设计与使用

param 模块是 glex 的第十个模块,为后台提供**运行时参数**能力:管理员在控制台上维护"键—值"参数,模块通过进程内读取契约向其他模块供值,改参数不用重新部署。它对标 RuoYi 的参数管理页(`sys_config`),是全部模块里最薄的一个——一张表、五个写读端点、一个跨模块契约,没有任何运行时机制(无缓存、无调度、无事件)。

本文记录设计定稿(2026-09-30,与用户逐问确认)及日常使用方式。

相关文档:`GLOSSARY.md` 的 param 术语节;沿用 [ADR 0001](../adr/0001-single-tenant.md)(单租户)、[ADR 0002](../adr/0002-audit-fields-on-every-table.md)(审计字段)、[ADR 0005](../adr/0005-application-calls-domain-services-not-repositories.md)(分层)、[ADR 0024](../adr/0024-listq-deep-module-for-list-queries.md)(列表)、[ADR 0025](../adr/0025-two-value-signatures-for-layer-facing-functions.md)(签名)。

## 1. 核心概念

| 概念 | 中文 | 本质 | 存活处 |
|---|---|---|---|
| **Runtime Parameter** | 运行时参数 | 一行数据:名称、键、值、内置标记、备注 | **数据库 `sys_config` 表** |
| **Built-in Parameter** | 内置参数 | `built_in=1` 的参数:可改值、不可删除;标记建后不可改 | 同上 |
| **ConfigReader** | 读取契约 | `crossdomain/param.ConfigReader`,其他模块按 key 取值的唯一入口 | **代码**,进程内接口 |

与静态配置的边界是本模块最重要的概念切割:**yaml 里的 `conf.Bootstrap` 是部署期静态配置,`sys_config` 是运行时参数**。一个值只允许有一个真相源——当前 `user.init_password` 活在 yaml,`sys_config` 就不播种它的同名键;静态开关迁进参数表的条件是"消费端同批切换到 Reader",绝不是先播一行长得像的行(详见第 6 章)。

## 2. 设计决策

每个决策都对应一个明确放弃的备选方案。

**① 治理自由:不做键注册制。** 管理员可自由增删改参数键,没有代码侧白名单。放弃注册制(代码声明合法 key)的理由:消费方寥寥,注册机制是为想象中的规模付复杂度;真出现垃圾键问题再收紧是可逆决定。`built_in` 防删:内置行拒绝删除、改值随意,但**标记本身建后不可改**(比 RuoYi 收紧——RuoYi 允许先把 Y 改 N 再删;glex 的读取契约声明"系统假设它存在",不允许 PATCH+DELETE 两步绕过)。

**② 值是纯字符串。** 无类型列、无结构化 JSON,布尔写成 `true`/`false`,解析归消费方。管理页按纯文本渲染。

**③ 键建后可改(PATCH 全字段放开)。** 与 dict 的 [ADR 0006](../adr/0006-dict-type-key-immutable.md) 相反,理由是耦合形态不同:dict_type 的 key 被 `sys_dict_data` 行在库内引用,改名直接断数据;config 的 key 只被消费代码按字面引用,且改名方与消费方都是人,改错的表现是消费方读到 not-found 而非数据损坏。改键同样过唯一性预检。若日后出现被多处消费的敏感键,收紧为不可改不破坏任何数据。

**④ 无缓存,直读 DB。** 放弃 RuoYi 的 Redis cache-aside + "刷新缓存"按钮:表小(个位到几十行)、单实例部署(ADR 0026)、读取走进程内契约,PG 点查便宜;dict 模块已确立无缓存先例。Reader 契约挡在消费方与实现之间,将来要加缓存是 repo 内部实现变更,契约与调用方零改动。

**⑤ 不暴露"按键读值"HTTP 端点。** RuoYi 的 `GET /system/config/configKey/{key}` 只要求登录不要求权限——任何登录用户可读任意参数值(包括初始密码明文)。glex 砍掉该端点:读取只走进程内 Reader 契约;管理页用带权限的列表/详情。将来登录页真需要读公开开关时,按白名单端点再加,是纯增量。

**⑥ 不做 Excel 导出。** 参数表几十行,列表页能看全;user 模块的导出机制(ADR 0017)摆在那里,想要照抄是纯增量。dict 已证明兄弟模块可以不带导出。

单租户(ADR 0001):`sys_config` 无租户维度,全局一份——RuoYi 的 sys_config 本就无 `tenant_id`。

## 3. 数据模型

一张表,ent schema(`internal/infra/ent/schema/param.go`,schema 类型叫 `Param`——ent 保留字 `config` 不可用)为真源,`docs/db/schema.sql` 人工镜像并手工执行;进程启动不改表。审计五件套走 mixin(ADR 0002),软删,无硬删。

```sql
-- ----------------------------
-- 参数配置表
-- ----------------------------
create table if not exists sys_config (
    id           bigint       not null,
    config_name  varchar(256) not null,
    config_key   varchar(256) not null,
    config_value varchar(500) not null default '',
    built_in     smallint     not null default 0,
    remark       varchar(500) not null default '',
    create_by    bigint       not null default 0,
    update_by    bigint       not null default 0,
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    deleted_at   timestamptz,
    constraint sys_config_pkey primary key (id)
);

comment on table sys_config is '参数配置表(运行时参数)';
comment on column sys_config.id is '雪花 ID';
comment on column sys_config.config_name is '参数名称';
comment on column sys_config.config_key is '参数键名,活跃行内唯一';
comment on column sys_config.config_value is '参数键值(纯字符串,解析归消费方)';
comment on column sys_config.built_in is '内置参数:1=不可删除;0=普通';
comment on column sys_config.remark is '备注';

create unique index param_config_key
    on sys_config (config_key) where deleted_at is null;
```

与 RuoYi 原版的差异均按仓库既有惯例:PK 叫 `id` 不叫 `config_id`;`config_type char(Y/N)` 归一为 `built_in smallint`(`is_frame/visible` 先例);去掉 `create_dept`(仓库不用);长度 name/key 对齐仓库统一的 varchar(256),value 保留 RuoYi 的 500 余量(值承载数据不是标签)。

索引只有一个:键的部分唯一索引(软删后释放键,ADR 0002 同款),它同时就是 Reader 按 key 点查的索引。列表查询走全量活跃集,不另建索引。

## 4. API 面

`api/param/v1/config.proto`(包名 `param` 避开 `internal/conf`/`configs` 的纠缠;聚合名沿用 config,与 dict 模块聚合叫 dict_type 同构):

| 操作 | HTTP | 权限 | 说明 |
|---|---|---|---|
| 列表 | `GET /iam/config` | `system:config:list` | listq:AIP-160 过滤 `config_name`/`config_key`/`built_in`/`created_at`,默认 `id desc` |
| 详情 | `GET /iam/config/{id}` | `system:config:query` | |
| 新增 | `POST /iam/config` | `system:config:add` | `built_in` 客户端可设(仅治理可删性);服务端过键唯一性预检 |
| 修改 | `PATCH /iam/config/{id}` | `system:config:edit` | field-mask 局部更新,`config_key` 可改,改键过唯一性预检;`built_in` 建后不可改,mask 携带无效 |
| 删除 | `DELETE /iam/config/{id}` | `system:config:remove` | 软删;`built_in=1` 拒绝 |

路由映射登记进 `internal/server/auth_routes.go`(操作名 → 权限串);审计字段由应用层从 Principal 盖戳,DTO 不携带。

## 5. 文件地图

```
api/param/v1/config.proto + error_reason.proto     HTTP API 与错误码(make api 生成 Go/TS/openapi)
internal/domain/param/
├── entity/config.go        Config 实体 + Validate(名称/键非空限长,键限 ^[A-Za-z0-9][A-Za-z0-9._-]*$,须字母/数字开头)
├── repository/config.go    ConfigRepo 端口 + DeleteRequest{ID, ActorID}
├── service/config.go       领域服务 Configs:键唯一性预检、内置删除保护
└── errors.go               sentinel:CONFIG_NOT_FOUND / CONFIG_KEY_CONFLICT / CONFIG_BUILT_IN_LOCKED / CONFIG_INVALID / CONFIG_STORE_UNAVAILABLE
internal/application/param/
├── config.go + provider.go  ConfigApplication:DTO 转换、AuditActor 盖戳、InTx 包写、logx.Propagate 面包屑
└── transaction.go           Transaction 端口(store.TxManager 适配)
internal/infra/param/
├── config.go               ent 仓储(listq 四行查询、log-and-map;ent 谓词包别名 entparam)
├── snowflake.go + provider.go
internal/infra/ent/schema/param.go     表结构:schema 类型叫 Param(ent 保留字 config 不可用),表名锁 sys_config,含 config_key 部分唯一索引(param_config_key)
internal/crossdomain/param/
├── contract.go             type ConfigReader interface { ValueByKey(ctx, key string) (string, error) }
├── impl/config_reader.go   契约实现:委托领域服务 FindByKey(直读,无缓存)
└── parammock/              mockgen 生成,消费方测试用
web/src/pages/iam/config/   管理页(列表 + 表单弹窗;内置行不渲染删除动作)
web/src/services/param/v1/  生成客户端(随 make api 生成);router/registry.tsx 注册组件
internal/server/auth_routes.go           +5 条权限映射
docs/db/schema.sql + seed.sql            DDL 镜像 + 种子
```

测试按仓库三层惯例:应用层手写 fake 领域服务;领域层内存 repo(返回 sentinel);infra 层 go-sqlmock + 真 ent client 校 SQL 形状(注意 ent 把写包在自己的事务里并事后重载行,Begin→UPDATE→SELECT→Commit)。

## 6. 读取契约与静态配置迁移

**契约**:`crossdomain/param.ConfigReader.ValueByKey(ctx, key) (string, error)`。缺失键返回 `CONFIG_NOT_FOUND` 哨兵;实现(`internal/crossdomain/param/impl/config_reader.go`)委托领域服务 FindByKey 直读 DB(决策④),装配方式与 crossdomain/audit 同款。ADR 0025 签名:标量进、单值出。

**消费方接入步骤**(以将来的 authn 注册开关为例):

1. `fx` 注入 `crossparam.Reader`;
2. 调 `ValueByKey(ctx, "sys.account.registerUser")`,not-found 哨兵当 `false` 处理(缺省即关闭,安全侧倒);
3. 布尔解析 `strconv.ParseBool`,解析失败按 Warn + 关闭处理——值是自由字符串(决策②),消费方必须容忍脏值。

**静态开关迁移纪律**:一个值一次只活在一处。迁移 = 同一批改动里"消费端切到 Reader + yaml 键删除 + (可选)`sys_config` 播种该键",三步一个事务性批次。当前待迁移清单:`user.init_password`(Excel 导入在用,ADR 0017)——它迁移之日,才是 `sys_config` 播种 `sys.user.initPassword` 之时。

## 7. 种子数据

`docs/db/seed.sql` 追加:

- **参数 2 行**(均 `built_in=1`):`sys.account.registerUser=false`(注册开关,休眠——消费端未接)、`sys.oss.previewListResource=true`(同上)。**不播** `sys.user.initPassword`(见第 6 章)。
- **菜单**:参数管理页(menu_type=2,`path: config`,`perms: system:config:list`,挂字典管理同一目录下)+ 4 个按钮行(menu_type=3,query/add/edit/remove,dict 先例:list 权限落在页面行);`sys_role_menu` 仅授 superadmin(RuoYi parity:参数管理不授 common),ID 沿用种子雪花字面量惯例。

## 8. 错误码

| 错误 | HTTP | 含义 |
|---|---|---|
| `CONFIG_NOT_FOUND` | 404 | 不存在或已软删 |
| `CONFIG_KEY_CONFLICT` | 409 | 活跃参数键重复(部分唯一索引兜底服务层友好预检) |
| `CONFIG_BUILT_IN_LOCKED` | 409 | 内置参数不能删除 |
| `CONFIG_INVALID` | 400 | 名称/键空或超长;键不匹配 ^[A-Za-z0-9][A-Za-z0-9._-]*$(须字母/数字开头) |
| `CONFIG_STORE_UNAVAILABLE` | 503 | 存储层意外故障(infra log-and-map,ADR 0007) |
