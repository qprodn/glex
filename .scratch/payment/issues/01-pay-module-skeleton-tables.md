# 01 — pay 模块骨架与四张表

**What to build:** pay 作为第 7 个模块在既有五处布局（api / application / domain / infra / crossdomain）上成型并纳入 fx 装配；四张表——Merchant Account（商户账号）、Payment（支付单）、Refund（退款单）、Channel Notification 流水（回调流水）——以 ent schema 与 schema.sql 落库。空 proto 服务可生成、服务可编译启动，不提供任何业务行为。细节参见 `.scratch/payment/design.md` §3–§4。

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [x] 四张表与设计文档 §4 一致并在数据库中创建：雪花 id、审计五字段、软删除、smallint 枚举、(channel, name) 与 (channel, notify_id) 的 partial unique 及各索引
- [x] 五处模块空壳 + 四个 fx Module 注册进装配；`make api` 与 `make all` 通过
- [x] 服务带空 pay 模块正常启动，既有六模块行为无回归
