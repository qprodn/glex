package wechat

import (
	"strings"

	"glex/internal/conf"
	wechatservice "glex/internal/domain/wechat/service"

	"go.uber.org/fx"
)

// Module contains the wechat application use cases.
var Module = fx.Module("wechat-app",
	fx.Provide(NewWechatApplication, provideCallbackBase),
)

func provideCallbackBase(cfg *conf.Wechat) wechatservice.CallbackBase {
	if cfg == nil {
		return ""
	}
	return wechatservice.CallbackBase(strings.TrimSpace(cfg.GetCallbackBaseUrl()))
}
