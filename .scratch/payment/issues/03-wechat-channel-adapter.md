# 03 — 微信支付 V3 适配器

**What to build:** 微信 Channel 的 ChannelGateway 全量实现：Native 预下单（返回 code_url）、查单、关单、退款、退款查询、Channel Notification 验签与解析（含 APIv3 报文解密）、Connectivity Test（拉取平台证书证明密钥链路可用）。SDK 客户端按（商户, 配置摘要）缓存，商户配置变更即失效；平台证书自动刷新由官方 SDK 承担。渠道外呼统一超时。细节参见 `.scratch/payment/design.md` §6。

**Blocked by:** 02 — 商户域：加密落库、动态表单、掩码回显。

**Status:** ready-for-agent

- [x] Prepay / Query / Close / Refund / QueryRefund / ParseNotification / Probe 全量实现，httptest 伪渠道契约测试覆盖报文映射、单位换算（渠道单位 ↔ 最小单位，ADR 0019）
- [x] 验签或解密失败返回错误，调用方据此拒绝处理
- [x] 客户端缓存键含配置摘要：更新商户配置后新配置立即生效，无需重启
- [x] 录入真实商户号后连通性测试通过（手工验收一次即可）
