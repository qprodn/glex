# 05 — 支付链路：下单、状态机、惰性过期

**What to build:** Payment 的完整生命周期：CreatePayment（防重锁 → 关闭同（商户, 业务单号）的 PENDING 旧单 → 预下单 → 回填二维码内容；预下单被渠道拒绝时该单即 FAILED 并记原因）并返回二维码内容；GetPayment（PENDING 且过期时触发 settle）/ ListPayments / QueryPayment（主动查单并推进，回调丢失的兜底）/ ClosePayment（渠道关单成功才置 CLOSED；遇渠道「已支付」错误转查单推进 PAID）。状态推进全部 CAS：PENDING → PAID / FAILED / CLOSED / EXPIRED，终态不可逆，乱序与重复为 no-op。金额为最小单位的 int64 加币种码，MVP 渠道强制 CNY（ADR 0019）。细节参见 `.scratch/payment/design.md` §5、§7。

**Blocked by:** 03 — 微信支付 V3 适配器；04 — 支付宝适配器。

**Status:** ready-for-agent

- [x] 微信、支付宝经同一用例各跑通：下单 → 扫码支付 → 主动查单 → PAID（补渠道交易号、付款方、支付时间）
- [x] 同（商户, 业务单号）重复下单自动关闭旧 PENDING 单再新建；并发重复提交被防重锁拦截为 409（既有全库语义）
- [x] PENDING 过期后详情 / 查询触发 settle：渠道已付 → PAID，未付 → EXPIRED；列表查询不触发任何渠道外呼
- [x] 关单遇渠道「已支付」转查单推进 PAID；PAID 后迟到的关单意图与重复推进均为 no-op
- [x] 预下单渠道失败的单即 FAILED 并记 fail_reason，不残留可支付假象
- [x] 状态机单测覆盖 CAS 竞争、乱序回执、重复推进
