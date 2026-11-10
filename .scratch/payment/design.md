# pay 模块：支付应用（ADR 0030）

> 决定日期：2026-11-10。权威是 ADR 0030，术语见 `GLOSSARY.md` 的 `### pay`。
> 仍有效：ADR 0018（凭据加密）、ADR 0019（金额 int64 分）、ADR 0020（渠道回调）、
> ADR 0021（配置为加密 JSON）、ADR 0022（支付完成事件只在渠道回调发布）。
> 代码里的 `Initiate` 仍会关掉同一商户、同一业务单号下的待支付单再插入新单。
> 本文描述确认后的模型，不是当前代码。

## 1. 定位

pay 回答三件事：

1. **后台配置商户账号**：运营录入一个渠道上的商户账号，凭据密文落库。
2. **支付应用**：一个调用方绑定恰好一个商户账号，持有自己的结果地址。
3. **统一收退款**：调用方用支付应用的编号发起支付和退款。渠道差异在适配器里。

抽象的验证标准不变：新增一个渠道 = 新增一个 adapter 包 + 一份 config schema，
不改核心表结构、不改 API、不改状态机。

## 2. 范围

**做**：

| 能力 | 说明 |
|---|---|
| 商户账号 | CRUD、启停、撤除、动态表单、掩码回显、连通性测试 |
| 支付应用 | 创建时绑定一个商户账号；显示名可改且未撤除者之间唯一；两条结果地址可空、可改；启停、撤除 |
| 统一下单 | 传入支付应用编号、业务单号、金额、支付方式。命中已有单则按第 5 节返回或拒绝 |
| 渠道回调 | 免鉴权验签 + write-once 流水 + 幂等状态推进（ADR 0020） |
| 主动查单 / 关单 / 惰性过期 | 与现在相同。这些路径把支付单收成已支付时只发业务通知 |
| 退款 | 多次部分退款，累计不超过已付。退款单第一次变成已退款或失败时发业务通知 |
| 业务通知 | 自动重试 9 次；用尽后后台可见；失败期间可按当前地址手工重发 |
| 渠道 | 微信支付 V3 + 支付宝。支付方式按笔选择，MVP 仍是扫码 |

**不做**：支付拓展单、把渠道和支付方式编进同一个编码、钱包、转账、应用签名、
给外部系统的查询接口、分账、对账、退款审批。Stripe / 云闪付仍只留枚举和 schema。

## 3. 模块布局

在现有 pay 五处布局上增加支付应用和业务通知，分层不变（ADR 0005）。

```
api/pay/v1/                         增加 PayApplication 与业务通知的 RPC
internal/application/pay/           application.go, business_notify.go
internal/domain/pay/entity/         application.go, business_notice.go
internal/domain/pay/service/        同上；支付发起改为按支付应用
internal/infra/pay/                 表对应的 repo；业务通知的 HTTP 与扫描
cmd/glex/app.go                     注册不变
```

`ChannelGateway` 仍是与 repository 平行的端口。业务通知不是渠道端口。

## 4. 表

沿用雪花 id、五审计字段、软删除、smallint 枚举、跨表引用为裸 bigint、不建外键。

### pay_application

```
id                  bigint pk
name                varchar(64) not null          -- 显示名
merchant_id         bigint not null              -- 创建时绑定，之后不可改
status              smallint not null default 0  -- 0 启用 1 停用
order_notify_url    varchar(512) not null default ''
refund_notify_url   varchar(512) not null default ''
remark              varchar(500) not null default ''
-- 部分唯一：(name) WHERE deleted_at IS NULL
-- 索引：(merchant_id) WHERE deleted_at IS NULL
```

撤除后名字可以再用。结果地址允许空字符串。

### pay_payment

在现有列上增加，并改掉业务单号的唯一性：

```
application_id   bigint not null     -- 支付应用
notify_url       varchar(512) not null default ''  -- 创建时抄下的支付结果地址
```

`merchant_id`、`channel` 仍在发起时从该支付应用的商户账号快照下来。
`id` 仍是渠道 `out_trade_no`。

去掉「同一商户可多笔」的含义。部分唯一：

```
(application_id, biz_no) WHERE deleted_at IS NULL
```

`(merchant_id, biz_no)` 不再是唯一键。两个支付应用绑同一个商户账号时，
相同的业务单号是两笔支付单。

### pay_refund

增加创建时抄下的退款结果地址：

```
notify_url    varchar(512) not null default ''
```

退款单号仍是渠道退款单号。一个支付单可以有多笔退款，累计不超过已付金额。
支付单状态不因退款改变，只累加 `refunded_amount`。

### pay_business_notice / pay_business_notice_attempt

这是业务通知，不是 `pay_notify_log`。渠道回调流水保持 ADR 0020，不混用。

```
pay_business_notice
  id               bigint pk
  application_id   bigint not null
  kind             smallint not null          -- 1 支付 2 退款
  payment_id       bigint not null
  refund_id        bigint                     -- kind = 2 时必填
  biz_no           varchar(64) not null       -- 正文用，创建时抄下
  notify_url       varchar(512) not null      -- 自动重试用的地址，创建时抄下
  status           smallint not null          -- 1 等待 2 已送达 3 已用尽
  attempt_count    int not null default 0     -- 已完成的自动尝试次数
  next_attempt_at  timestamptz                -- 等待中才有意义
  -- 一笔支付或一笔退款最多一条：部分唯一 (kind, payment_id) 或 (kind, refund_id)
  -- WHERE deleted_at IS NULL

pay_business_notice_attempt
  id            bigint pk
  notice_id     bigint not null
  manual        boolean not null              -- false 自动，true 手工
  http_status   int                           -- 没拿到响应则为空
  error         varchar(512) not null default ''
  accepted      boolean not null              -- HTTP 2xx
  created_at    timestamptz not null
```

地址在创建支付单或退款单时是空的，不插入 `pay_business_notice`。

## 5. 发起与状态

**支付单**终态仍是已支付、失败、已关闭、已过期。推进仍是
`WHERE status = 待支付` 的 CAS。

再次发起，锁键改为 `pay:prepay:{application_id}:{biz_no}`：

| 已有单 | 本次请求 | 结果 |
|---|---|---|
| 无 | — | 插入待支付单，抄下当前支付结果地址，然后预下单 |
| 有，金额与支付方式都相同 | — | 返回该单，不新开。若仍是待支付且还没有渠道载荷，对这一单补做预下单 |
| 有，金额或支付方式不同 | — | 拒绝 |

标题和过期时间以第一笔为准。已关闭、已过期、已失败的单也占住业务单号。
要换支付方式或再收一笔，调用方使用新的业务单号。

支付应用停用、撤除，或其商户账号停用、撤除时，不能新发起支付或退款。

**退款**仍要求支付单已支付，且累计不超过已付。创建时抄下当前退款结果地址。
同步返回的渠道结果和回调、查单走同一套 CAS。

**谁把支付单收成已支付，谁发什么：**

| 路径 | 支付完成事件 | 业务通知 |
|---|---|---|
| 渠道回调，且这次 CAS 从待支付变为已支付 | 发布，正文含支付应用编号 | 地址非空则建立通知 |
| 打开支付单、按业务单号读、主动查单、过期扫描、关单时发现已支付 | 不发布 | 地址非空则建立通知 |
| 渠道回调重复投递，单已经是已支付 | 不发布 | 不新建 |

读取和按业务单号读取只在「待支付且已过期」时问渠道，与现在相同。
主动查单对任何待支付单问渠道。关单只在渠道回答已经付了时改去查单。

退款单第一次变成已退款或失败时建立业务通知，不论是回调、查单还是当场返回。
支付失败、关闭、过期不建立业务通知。

## 6. 业务通知

正文是 JSON，不签名：

- 支付：`{"biz_no","payment_id"}`
- 退款：`{"refund_id","payment_id"}`

HTTP 2xx 为这次尝试成功，不解析正文。超时、连不上、其他状态码都算失败。

自动尝试连同第一次共 9 次。第一次在状态落稳之后立刻发出，不放进调用方事务，
也不放进渠道回调的处理事务里。进程若在第一次之前退出，行已经在，
`next_attempt_at` 为现在，由扫描补上。之后的间隔是 15 秒、15 秒、30 秒、
3 分钟、30 分钟、30 分钟、30 分钟、60 分钟。第 9 次仍失败则状态改为已用尽。

扫描是一条定时任务（ADR 0023）。周期不粗于 15 秒，只领取
`status = 等待 AND next_attempt_at <= now` 的行。

手工重发是后台接口，一次调用打一回：

- 只接受已用尽的通知。已送达的、仍在自动重试中的，拒绝。
- 地址用支付应用上**当前**对应的那条。支付用支付结果地址，退款用退款结果地址。
  当前地址为空则拒绝。自动重试始终用通知行上抄下的地址。
- 2xx：状态改为已送达。否则保持已用尽，可以再点。不重新开始 9 次自动间隔。
- 支付应用已停用或已撤除，已用尽的通知仍可重发。

每一次自动或手工尝试都插入 `pay_business_notice_attempt`。
后台列出已用尽的通知，并带上最后一次结果。成功的尝试留在记录里，不进这个队列。

接收方在本服务内读取支付单或退款单。业务通知不是入账依据。
这一轮没有给外部系统的查询接口。

## 7. 渠道回调与适配器

渠道回调仍是 `POST /pay/notify/{channel}/{merchant_id}`。
多个支付应用共用一个商户账号时，渠道打同一个地址，再按支付单号找到支付应用。

处理序、验签、write-once、方言 ACK、连通性测试、配置加密和渠道 schema
保持 ADR 0018、0020、0021 与现有适配器，不因支付应用改动。

`ChannelGateway` 的 `Prepay`、`Query`、`Close`、`Refund`、`QueryRefund`、
`ParseNotification`、`Probe` 不变。`NotifyURL` 仍是
`/pay/notify/{channel}/{merchant_id}`。

## 8. API 与 crossdomain

支付应用（登录态，`pay:application:*`）：

| RPC | 说明 |
|---|---|
| CreatePayApplication | `name`、`merchant_id`、两条结果地址。商户账号必须启用且未撤除 |
| UpdatePayApplication | 可改 `name` 和两条地址。不能改 `merchant_id` |
| ChangePayApplicationStatus | 启停 |
| WithdrawPayApplication | 软删 |
| Get / List | 列表可按商户账号、状态过滤 |

发起支付的请求改为 `application_id`、`biz_no`、`subject`、`amount`、`pay_method`、
`expire_minutes`。不再传 `merchant_id`。

按业务单号读取改为 `GetByBizNo(application_id, biz_no)`，最多一笔。

crossdomain 的 `PaymentCreator` / `PaymentReader` 使用同一套请求。
错误仍是 pay 的 reason。

业务通知的手工重发：`POST /pay/business-notice/{id}/resend`，
权限 `pay:business-notice:resend`。列表权限 `pay:business-notice:list`。

## 9. 安全

- 商户凭据仍只以密文落库，API 只出掩码。
- 渠道回调仍以验签为鉴权（ADR 0020）。
- 业务通知不签名。调用方必须回读支付单或退款单。
- 金额仍拒绝 ≤ 0，MVP 仍强制 CNY。

## 10. 测试

在现有状态机、加密、渠道报文测试之外增加：

- 同一支付应用、同一业务单号：金额和支付方式相同则返回原单；任一项不同则拒绝；终态仍占号。
- 两个支付应用绑同一商户账号，相同业务单号得到两笔支付单。
- 创建后改绑商户账号被拒绝。显示名在未撤除的支付应用之间不能重复，撤除后可以复用。
- 渠道回调第一次收成已支付：发布支付完成事件，并在地址非空时建立业务通知。
- 查单、过期读取、过期扫描、关单发现已支付：不发布事件；地址非空时建立业务通知。
- 自动尝试满 9 次后为已用尽；手工重发打当前地址；当前地址为空则拒绝；2xx 后不再接受重发；失败可再次手工重发，且不重置自动间隔。
- 创建时地址为空：不建立通知；事后补地址也不补发。

## 11. 还没落地

本文不改代码。下一步是按第 4–8 节改表、发起路径、事件字段和业务通知，
并让 `Initiate` 停止「关旧单再插新单」。
