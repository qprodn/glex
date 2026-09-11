# 02 — 商户域：加密落库、动态表单、掩码回显

**What to build:** 后台可以通过 API 管理 Merchant Account：按渠道（微信 / 支付宝）的 config schema 校验并录入配置；凭据整块 AES-256-GCM 密文落库（ADR 0018、ADR 0021），主密钥只来自环境变量 `PAY_MASTER_KEY`；列表与详情只回掩码回显，任何 API 不回明文；启停与撤除（软删）生效；渠道 schema 查询端点供前端渲染动态表单；Connectivity Test（连通性测试）端点就位，未注册适配器的渠道返回「渠道未接入」业务错误。细节参见 `.scratch/payment/design.md` §6–§7。

**Blocked by:** 01 — pay 模块骨架与四张表。

**Status:** ready-for-agent

- [ ] 新建 / 更新商户：schema 校验通过后配置以密文落库，库中任何列无明文凭据；写入时同步生成掩码回显与配置摘要
- [ ] 列表与详情只返回掩码回显；显示路径永不解密
- [ ] 渠道 schema 端点返回两渠道完整字段定义，敏感字段带标记
- [ ] `PAY_MASTER_KEY` 缺失时商户相关用例返回明确哨兵错误，服务其余模块不受影响
- [ ] operlog 对敏感字段脱敏，schema 中 secret 标记的字段自动并入脱敏名单
- [ ] 停用 / 撤除的商户不能再被发起支付引用
- [ ] 单测覆盖：加解密往返、掩码生成、schema 校验、缺钥路径
