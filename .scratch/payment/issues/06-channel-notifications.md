# 06 — 回调链路：免鉴权路由、回调流水、方言 ACK

**What to build:** Channel Notification（渠道回调）端点上线：按（渠道, 商户）寻址的免鉴权路由——验签即鉴权——挂最小 Redis 固定窗口限流；每条通知先落 write-once 回调流水（原文、验签相关 headers、验签与处理结果），(渠道, notify_id) 唯一约束吸收渠道的重投；随后按类别 CAS 推进 Payment 状态，最后以渠道方言 ACK（微信 v3 JSON `{"code":"SUCCESS"}`、支付宝字面 `success`）。端点进 operlog skip 名单（无 Principal、高流量，流水行本身即记录，ADR 0020）。细节参见 `.scratch/payment/design.md` §7 回调端点一节。

**Blocked by:** 05 — 支付链路：下单、状态机、惰性过期。

**Status:** ready-for-agent

- [ ] 金路径集成测试：伪渠道投递支付成功通知 → 流水落库 → 状态 PAID → 渠道方言 ACK
- [ ] 同一 (渠道, notify_id) 重投直接 ACK，不重复处理、不改状态
- [ ] 验签失败：落 verified=false 的流水并拒绝，无任何状态变更，Warn 记录
- [ ] 未注册商户的路径返回 404；限流对高频投递生效
- [ ] 端点无需登录态，加入免鉴权名单后既有六模块的鉴权链路无回归
