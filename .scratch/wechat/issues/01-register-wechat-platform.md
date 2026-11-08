# 01: 登记微信平台

**What to build:** 运营可以在后台登记一条微信平台，之后只看到密钥掩码，可以改名称、更换密钥、撤回一条还没有应用挂着的平台。未撤回的平台之间，同一个 component AppID 只能有一条。

**Blocked by:** None（可以马上开始）

**Status:** ready-for-agent

- [x] 运营可以登记微信平台，填写显示名称、component AppID、component secret、Token 和 EncodingAESKey
- [x] 登记之后的查询只返回这些秘密的掩码，响应里不再出现明文
- [x] 运营可以修改显示名称
- [x] 运营可以更换 component secret、Token 和 EncodingAESKey；component AppID 在这条平台的存续期间不能改
- [x] 未撤回的平台里，相同的 component AppID 被拒绝
- [x] 平台可以撤回；撤回后的平台不出现在日常列表里
- [x] 撤回之后，同一个 component AppID 可以再登记成一条新的平台
- [x] 环境里没有 `WECHAT_MASTER_KEY` 时，登记和更换密钥被拒绝，秘密不以明文保存
- [x] 操作日志里的密钥字段已被擦除
- [x] 后台出现「微信管理 / 微信平台」。权限是 `wechat:platform:list`、`create`、`update`、`remove`，并授予超级管理员
- [x] 上述行为由微信领域服务测试覆盖：内存仓储，断言运营能观察到的结果，不断言 SQL 或密文字节
