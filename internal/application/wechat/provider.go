package wechat

import "go.uber.org/fx"

// Module contains the wechat application use cases.
var Module = fx.Module("wechat-app",
	fx.Provide(NewWechatApplication),
)
