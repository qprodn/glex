# 08 — 后台页面：商户管理 + 菜单种子

**What to build:** 后台商户管理页：选择渠道后按其 config schema 渲染动态表单；敏感字段只展示掩码、留空提交即「保持原值」；Connectivity Test 按钮展示成功 / 失败与原因；商户启停与撤除操作。同时「支付管理」目录与全部 perms（`pay:merchant:*`、`pay:payment:*`、`pay:refund:*`）随种子落地，为后续页面占位。细节参见 `.scratch/payment/design.md` §7、§9。

**Blocked by:** 02 — 商户域：加密落库、动态表单、掩码回显。（与 03–07 并行。）

**Status:** ready-for-agent

- [ ] 从渠道 schema 动态渲染微信、支付宝两套表单并成功新建 / 更新商户
- [ ] 详情只展示掩码；敏感字段留空提交不覆盖已存值
- [ ] 连通性测试按钮展示结果与失败原因
- [ ] 菜单与按钮权限种子生效：无权限用户页面不可见、接口不可调
