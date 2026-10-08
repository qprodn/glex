# pay 模块架构与接口设计（MVP）

> 决策来源：2026-09-10 grill-with-docs 设计会话（两轮共 16 项决策全部确认）。
> 关联决策：ADR 0018（商户凭据加密落库）、ADR 0019（金额 int64 最小单位）、
> ADR 0020（渠道回调免鉴权路由 + 回调流水）、ADR 0021（配置为加密 JSON 块）。
> 术语见 `GLOSSARY.md` `### pay`。

## 1. 定位

pay 是 glex 的第 7 个模块（首个非 RuoYi 移植、原生设计的模块），落在现有单服务内，
沿用既有五处布局与装配方式。它回答两件事：

1. **后台配置**：运营在后台录入 Channel 的 Merchant Account（商户号、密钥、证书），
   凭据密文落库，配置完成即可收款；
2. **统一收退款**：业务方（当前是后台运营，未来是业务模块）通过统一接口发起
   Payment / Refund，渠道差异全部被适配器吸收。

抽象的验证标准：**新增一个渠道 = 新增一个 adapter 包 + 一份 config schema，
不改核心表结构、不改 API、不改状态机。**

## 2. MVP 范围

**做**：

| 能力 | 说明 |
|---|---|
| 商户管理 | Merchant Account CRUD、启停、动态表单（按渠道 schema）、掩码回显、连通性测试 |
| 统一下单 | CreatePayment（Native 扫码），返回二维码内容，页面渲染 |
| 渠道回调 | 免鉴权验签路由 + write-once 回调流水 + 幂等状态推进 |
| 主动查单 | QueryPayment：向渠道查询并推进状态（回调丢失的兜底） |
| 关单 | ClosePayment（手动）+ 重复发起自动关旧单 + 惰性过期 |
| 退款 | CreateRefund（多次部分退款，累计 ≤ 已付）、退款回调、QueryRefund |
| 渠道 | 微信支付 V3（`wechatpay-go`）+ 支付宝（`smartwalle/alipay`） |
| 业务契约 | crossdomain 查询/发起契约，供未来业务模块进程内调用 |

**明确不做（设计预留位）**：Stripe / 云闪付实现（枚举与 schema 留位）、对账单下载核对、
分账、转账/提现、退款审批流、开放 API（应用签名）、业务回调转发（notify 路由表）、
定时任务（过期靠惰性判定）、Excel 导入导出。

## 3. 模块布局

```
api/pay/v1/                        pay.proto, error_reason.proto (make api 生成)
internal/application/pay/          merchant.go, payment.go, refund.go, notify.go,
                                   provider.go, transaction.go   (事务边界在此层)
internal/domain/pay/entity/        payment.go, refund.go, merchant.go, notification.go
internal/domain/pay/service/       payment.go, refund.go, merchant.go,
                                   channel.go (ChannelGateway 端口)
internal/domain/pay/repository/    端口
internal/domain/pay/errors.go      域哨兵错误
internal/infra/pay/                repo 实现 + provider.go
internal/infra/pay/crypt/          AES-256-GCM 信封（ADR 0018）
internal/infra/pay/adapter/wechat/ 微信 V3 适配器（含 config schema）
internal/infra/pay/adapter/alipay/ 支付宝适配器（含 config schema）
internal/infra/pay/adapter/stripe/ 二期；schema 可先行
internal/crossdomain/pay/          contract.go, model/, impl/, paymock/
cmd/glex/app.go                    注册四个 Module
```

分层不变：api handler → application → domain service → repository 端口 → infra（ADR 0005）。
ChannelGateway 是与 repository 平行的外部网关端口，实现在 infra 的 adapter 包。

## 4. 领域模型与表（ent schema + `docs/db/schema.sql`）

沿用全库约定：雪花 id、AuditMixin 五审计字段、软删除、smallint 枚举、
跨模块引用为裸 int64 列不建 FK。表名前缀 `pay_`（非 RuoYi 血统，不用 `sys_`）。

```
pay_merchant_account
  id              bigint pk                 -- 雪花
  channel         smallint not null         -- 1 微信 2 支付宝 3 云闪付 4 Stripe
  name            varchar(64) not null      -- 显示别名
  status          smallint not null default 0   -- 0 启用 1 停用（RuoYi 惯例）
  config_cipher   text not null             -- 渠道配置 JSON 密文（ADR 0018/0021）
  config_masked   jsonb not null            -- 写入时生成的掩码回显（明文存放，仅掩码）
  config_digest   varchar(64) not null      -- 明文配置 sha256，客户端缓存失效用
  key_version     int not null default 1    -- KEK 版本（轮换预留）
  remark          varchar(500)
  -- 唯一：partial unique (channel, name) WHERE deleted_at IS NULL

pay_payment
  id               bigint pk                -- 即支付单号，字符串形式作渠道 out_trade_no
  merchant_id      bigint not null
  channel          smallint not null
  biz_no           varchar(64) not null     -- 业务单号（引用，非唯一）
  subject          varchar(256) not null
  amount           bigint not null          -- 最小单位（分），ADR 0019
  currency         char(3) not null default 'CNY'
  pay_method       smallint not null default 1   -- 1 Native（2 JSAPI 3 H5 4 App 预留）
  status           smallint not null default 1   -- 1 PENDING 2 PAID 3 FAILED
                                             --    4 CLOSED 5 EXPIRED
  channel_trade_no varchar(64)              -- 微信 transaction_id / 支付宝 trade_no
  channel_payload  text                     -- 预下单返回（code_url 等），供二维码重渲染
  payer_id         varchar(64)              -- openid / buyer_id（回调带回）
  refunded_amount  bigint not null default 0    -- 维护字段，退款推进时累加
  fail_reason      varchar(256)
  expire_time      timestamptz not null     -- 默认下单 +30 分钟
  paid_at          timestamptz
  -- 索引：(merchant_id, biz_no) partial、(status)、(created_at)、(channel_trade_no)

pay_refund
  id                bigint pk               -- 即退款单号（out_refund_no）
  payment_id        bigint not null
  merchant_id       bigint not null
  channel           smallint not null
  amount            bigint not null         -- 本次退款金额（分）
  reason            varchar(256)
  status            smallint not null default 1   -- 1 PENDING 2 SUCCESS 3 FAILED
  channel_refund_no varchar(64)
  fail_reason       varchar(256)
  refunded_at       timestamptz
  -- 索引：(payment_id)

pay_notify_log                                     -- write-once，ADR 0020
  id           bigint pk
  channel      smallint not null
  notify_id    varchar(128) not null       -- 渠道侧通知标识（微信 v3 header id / 支付宝 notify_id）
  merchant_id  bigint not null
  kind         smallint not null           -- 1 支付 2 退款
  target_no    varchar(64) not null        -- 支付单号或退款单号
  verified     boolean not null
  processed    boolean not null            -- 幂等命中（重复投递）为 false
  raw_body     text not null
  sign_headers jsonb                       -- 验签相关 headers，取证用
  received_at  timestamptz not null default now()
  -- 唯一：partial unique (channel, notify_id) WHERE deleted_at IS NULL
  -- create_by = 0（渠道无 Principal，按"系统自身"记录）
```

## 5. 状态机

**Payment**（终态不可逆，退款不改支付单状态，只累加 `refunded_amount`）：

```
PENDING ──回调/查单确认──▶ PAID
   │──回调/查单明确失败──▶ FAILED
   │──手动关单/重复发起自动关──▶ CLOSED
   └──惰性过期（now > expire_time 时结算路径发现）──▶ EXPIRED
```

- 推进一律 CAS：`UPDATE ... SET status=new WHERE id=? AND status=1`，影响行数=0
  即幂等命中（乱序回执如「成功后到关单」是 no-op，不是错误）。
- **惰性过期**：`settle(payment)` 在 GetPayment / QueryPayment 触发——PENDING 且已过期时
  先向渠道 Query（回调丢失时渠道可能已成功），仍未付则 Close 后标 EXPIRED。
  列表查询不触发（不做逐行外呼）。写读结算（write-on-read）是有意为之，ADR 见
  设计会话；引入 cron 是二期对账的伴生需求。
- PAID 后唯一允许的变更：`refunded_amount` 累加与 `payer_id`/`paid_at` 补录（CAS 各自独立）。

**Refund**：`PENDING ─▶ SUCCESS | FAILED`，同样 CAS。支付宝当面付退款是同步返回，
回填仍走同一状态机（PENDING→立即结算）。

## 6. 渠道网关端口与适配器

### 端口（`internal/domain/pay/service/channel.go`，infra 实现）

```go
type ChannelGateway interface {
    Channel() entity.Channel

    Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error)
    Query(ctx context.Context, m *entity.MerchantAccount, paymentNo string) (ChannelPaymentState, error)
    Close(ctx context.Context, m *entity.MerchantAccount, paymentNo string) error
    Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error)
    QueryRefund(ctx context.Context, m *entity.MerchantAccount, refundNo string) (ChannelRefundState, error)
    // 验签 + 解密 + 解析为规范事件；验签失败返回错误，调用方直接拒绝
    ParseNotification(ctx context.Context, m *entity.MerchantAccount,
        headers http.Header, body []byte) (entity.ChannelNotification, error)
    // 连通性测试：一次廉价的已鉴权渠道调用，不动钱
    Probe(ctx context.Context, m *entity.MerchantAccount) error
}

type PrepayRequest struct {
    Merchant  *entity.MerchantAccount // 已解密配置
    PaymentNo string                  // 雪花串，即 out_trade_no
    Amount    int64                   // 分（ADR 0019），渠道单位换算在 adapter 内
    Currency  string                  // MVP 强制 CNY，usecase 校验
    Subject   string
    ExpireAt  time.Time
    Method    entity.PayMethod        // MVP: Native
    NotifyURL string                  // /pay/notify/{channel}/{merchant_id}
}

type ChannelPaymentState struct {
    State          entity.PaymentStatus // PENDING/PAID/FAILED/CLOSED
    ChannelTradeNo string
    PayerID        string
    PaidAt         time.Time
    FailReason     string
}
```

### 注册与缓存（infra）

```go
// internal/infra/pay/adapter — 每渠道一个工厂；schema 静态暴露
type Factory interface {
    Channel() entity.Channel
    ConfigSchema() config.Schema          // 字段定义：类型/分组/校验/敏感标记/掩码规则
    NewGateway(cfg map[string]string) (service.ChannelGateway, error)
}
```

- `GatewayRegistry`（domain 端口、infra 实现）按 `(merchant_id, config_digest)`
  缓存已构建的 gateway；商户更新/停用/撤除即失效。SDK client 构建成本高（微信需拉
  平台证书），必须缓存。
- SDK 只出现在 adapter 包内：微信 `wechatpay-go`（平台证书自动刷新由其 certdownloader
  承担）、支付宝 `smartwalle/alipay`（证书模式自动轮换）。Stripe 二期 `stripe-go`；
  云闪付无成熟 Go SDK，二期按银联 acp 报文自写 HTTP。
- 渠道外呼统一 10s 超时（adapter 内 context）。

### 各渠道 config schema（字段即动态表单）

- **微信 V3**：`mchid`、`appid`（Native 也建议填，预留 JSAPI）、`api_v3_key`、
  `merchant_serial`（证书序列号）、`merchant_private_key`（PEM，textarea）。
- **支付宝**：`app_id`、`mode`（key / cert）、`app_private_key`、公钥模式配
  `alipay_public_key`，证书模式配 `alipay_cert_sn`/`alipay_public_cert`/`alipay_root_cert`。
- **Stripe（二期，schema 先行）**：`secret_key`、`webhook_secret`。

敏感字段（`secret: true`）写入密文块、回显只出掩码、operlog 脱敏名单同步收录。

### 加密（`internal/infra/pay/crypt`，ADR 0018）

- AES-256-GCM，密文格式 `key_version:nonce_base64:ciphertext_base64`；
  主密钥 32 字节来自环境变量 `PAY_MASTER_KEY`（base64），不进 config.yaml；
  fx 提供单例，缺钥时 cipher 可构造但一切加解密调用返回哨兵 `ErrMasterKeyMissing`
  ——商户相关用例全部报错（Warn），登录等无关路径不受影响。
- repo 边界加解密：entity 持明文（仅内存），落库为 `config_cipher`；
  写入时同步生成 `config_masked`（列表/详情读掩码，永不解密）与 `config_digest`。

## 7. 应用用例与 API（`api/pay/v1/pay.proto`，路由前缀 `/pay`）

### 商户管理（登录态，perms `pay:merchant:*`）

| RPC | 路由 | 说明 |
|---|---|---|
| CreateMerchantAccount | POST /pay/merchant | channel + name + 表单值；schema 校验、加密落库 |
| ListMerchantAccounts | GET /pay/merchant | AIP filter/page；回显 config_masked |
| GetMerchantAccount | GET /pay/merchant/{id} | 含掩码与 digest |
| UpdateMerchantAccount | PATCH /pay/merchant/{id} | 整体替换表单值；敏感字段留空 = 保持原值（schema 语义） |
| ChangeMerchantAccountStatus | PUT /pay/merchant/{id}/status | 启停；停用即摘除 gateway 缓存 |
| WithdrawMerchantAccount | DELETE /pay/merchant/{id} | 软删；摘缓存。有进行中支付不阻止（支付单已有快照渠道），仅禁止新发起 |
| ProbeMerchantAccount | POST /pay/merchant/{id}/probe | 连通性测试：微信拉平台证书；支付宝查一个不存在单号、以 `ACQ.TRADE_NOT_EXIST` 证签名链路通；Stripe 查余额 |
| GetChannelSchemas | GET /pay/channel-schemas | 全渠道 schema（前端动态表单渲染源） |

### 支付（perms `pay:payment:*`）

| RPC | 路由 | 说明 |
|---|---|---|
| CreatePayment | POST /pay/payment | `merchant_id, biz_no, subject, amount(分), pay_method, expire_minutes(默认30)` |
| GetPayment | GET /pay/payment/{id} | 纯读 + PENDING 且过期时触发 settle |
| ListPayments | GET /pay/payment | 按 status/biz_no/merchant/channel/时间过滤 |
| QueryPayment | POST /pay/payment/{id}/query | 主动查渠道并 CAS 推进（settle 的显式入口） |
| ClosePayment | POST /pay/payment/{id}/close | 仅 PENDING；渠道关单 + CLOSED |

CreatePayment 流程：`Locker.WithLock(pay:prepay:{merchant_id}:{biz_no})` →
关闭同 (merchant, biz_no) 的 PENDING 旧单 → 落 PENDING 行 → `Prepay` →
回填 `channel_payload` → 返回支付单 + 二维码内容（前端渲染）。
并发重复提交由锁兜底（`ErrLockBusy` → 409，全库既定语义）。

### 退款（perms `pay:refund:*`）

CreateRefund `POST /pay/refund`：`payment_id, amount, reason`；
`Locker.WithLock(pay:refund:{payment_id})` → 校验支付单 PAID 且
`refunded_amount + amount ≤ amount` → 落 PENDING → `Refund` → 回调/同步结果推进。
GetRefund / ListRefunds / QueryRefund 同型。

### 回调端点（免鉴权，非 proto）

`POST /pay/notify/{channel}/{merchant_id}` —— 在 `internal/server` 手工注册
（沿用 `registerFileTransferHTTP` 先例），进 auth 免鉴权名单与 operlog skip set，
挂新写的最小 Redis 固定窗口限流中间件（仅此路由组使用）。

处理序（ADR 0020）：读 body → 按 path 取商户（含解密，取不到 404）→
`ParseNotification` 验签（失败：落 `verified=false` 流水 + Warn + 拒绝）→
insert `pay_notify_log`（(channel, notify_id) 唯一冲突 = 重复投递，直接 ACK）→
按 kind CAS 推进支付单/退款单（补 channel_trade_no、payer_id、paid_at、
refunded_amount）→ 按渠道方言 ACK（微信 v3 `{"code":"SUCCESS"}`、支付宝字面 `success`）。

## 8. crossdomain 契约（`internal/crossdomain/pay`）

```go
// 发起支付（未来业务模块进程内调用；当前后台 API 与其共用 application 用例）
type PaymentCreator interface {
    CreatePayment(ctx context.Context, req *model.PaymentRequest) (*model.Payment, error)
}

// 查询（支付结果按需拉取；notify 转发是二期演进，ADR 会话已记）
type PaymentReader interface {
    GetPayment(ctx context.Context, paymentNo string) (*model.Payment, error)
    GetByBizNo(ctx context.Context, merchantID int64, bizNo string) (*model.Payment, error)
}
```

错误以 pay 的 kratos reason 跨缝（全库惯例）。fx Module 注册于
`cmd/glex/app.go`，与四层 Module 并列。

## 9. 安全清单

- 凭据仅密文落库（ADR 0018）；明文只在内存构建 gateway 时存在；API 只出掩码。
- operlog 脱敏名单扩展：`merchant_private_key, api_v3_key, secret_key,
  app_private_key, alipay_public_key, alipay_public_cert, alipay_root_cert,
  webhook_secret` 等（schema `secret: true` 字段自动并入）。
- 回调路由：验签即鉴权 + 限流（ADR 0020）；通知流水与操作日志互不替代。
- 金额服务端校验：CreatePayment 拒绝 ≤0、MVP 渠道强制 CNY；金额运算只在
  usecase/domain 内做（累计退款上限）。
- 权限：`pay:merchant:{list,create,update,remove,probe}`、
  `pay:payment:{list,create,query,close}`、`pay:refund:{list,create,query}`；
  菜单与按钮 perms 随 seed.sql 播种（目录「支付管理」→ 商户/支付单/退款单三页）。

## 10. 测试策略

- **单元**：状态机 CAS 推进（含乱序/重复）、退款累计上限、config schema 校验与
  掩码生成、crypt 往返与 `ErrMasterKeyMissing`、notify 去重（唯一冲突路径）。
- **adapter**：httptest 伪渠道服务驱动契约测试（微信 v3 用固定测试证书/密钥构造
  可验签的报文；支付宝用测试密钥对），覆盖 Prepay/Query/Refund/ParseNotification/
  Probe 的报文映射与单位换算。
- **集成**：repo 层按既有模块模式（真实 PG）；locker 语义复用 miniredis 既有设施。
- 回调全链路一条金路径测试：伪渠道投递 → 流水落库 → 状态推进 → 方言 ACK。

## 11. 阶段划分（"当前适合做到哪一步"的答案）

**当前做到**：第 1 阶段全量（本文档 §2 的 MVP 表），并在枚举、schema、
接口签名上为 Stripe / 云闪付留好位置——抽象以两个真实渠道落地验证。

| 阶段 | 内容 | 触发条件 |
|---|---|---|
| 1（本次） | 微信 + 支付宝，Native，商户管理/收/退全链路 | — |
| 2 | Stripe、云闪付 adapter；cron 过期/对账单核对；notify 转发（业务回调路由表）；开放 API（应用签名+限流）；列表 Excel 导出 | 出海收款 / 渠道商求 / 出现外部调用方 / 对账人力成本显现 |
| 3 | 服务商（ISV）模式与子商户；分账；转账/提现；退款审批流 | 真实业务形态出现 |

**为什么不现在做二期**：系统当前没有 C 端流量、没有外部调用方、没有消息队列与
cron 基础设施——开放 API 与 notify 转发的复杂度不亚于支付本身，无消费者时全是
负资产；对账需要真实账单量才有意义。

## 12. 工单拆分建议（`.scratch/payment/`，tracer-bullet 顺序）

1. **表与骨架**：4 张 ent schema + schema.sql + 五处空模块 + fx 注册（可编译）。
2. **crypt + 商户域**：加密设施、schema 定义（微信/支付宝）、商户 CRUD/启停/Probe
   用例与 API（密文落库、掩码回显），不含真实渠道（Probe 先桩）。
3. **微信 adapter**：gateway 实现 + httptest 契约测试 + Probe 接真。
4. **支付宝 adapter**：同 3——两个 adapter 落地即抽象验证。
5. **支付链路**：CreatePayment（锁+关旧+预下单）/Get/List/Close/Query + settle。
6. **回调链路**：notify 端点（免鉴权+限流+流水+CAS+方言 ACK）+ 金路径测试。
7. **退款链路**：CreateRefund/Query/退款回调 + 上限测试。
8. **web 页面 + 种子**：商户管理（动态表单+Probe 按钮）、支付单（发起弹窗二维码、
   详情、关单）、退款页；菜单/权限 seed.sql。
