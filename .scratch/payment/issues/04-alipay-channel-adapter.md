# 04 — 支付宝适配器

**What to build:** 支付宝 Channel 的 ChannelGateway 全量实现（与 03 同构）：当面付扫码预下单、查单、关单、退款（同步语义映射为「创建后立即可结算」）、退款查询、Channel Notification 验签与解析、Connectivity Test（查询一个不存在的单号，以收到 `ACQ.TRADE_NOT_EXIST` 判定请求签名与验签链路通）。支持公钥与证书两种模式，证书自动轮换由 SDK 承担。细节参见 `.scratch/payment/design.md` §6。

**Blocked by:** 02 — 商户域：加密落库、动态表单、掩码回显。（与 03 互相独立，可并行。）

**Status:** ready-for-agent

- [x] Gateway 全量方法实现，httptest 伪渠道契约测试覆盖表单签名、验签、notify_id 提取、单位换算
- [x] 验签失败返回错误，调用方据此拒绝处理
- [x] 录入真实支付宝应用后连通性测试通过（手工验收一次即可）
- [x] 与微信适配器共存于同一注册表：按渠道分发，核心模型与用例零改动——双渠道落地即抽象验证达成
