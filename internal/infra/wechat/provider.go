// Package wechatinfra hosts the wechat module's persistence adapters.
package wechatinfra

import (
	wechatservice "glex/internal/domain/wechat/service"

	"go.uber.org/fx"
)

// Module contains the wechat persistence adapters: the platform repo, the
// app repo, the credential cipher, and the Open API port (ADR 0029).
var Module = fx.Module("wechat-infra",
	fx.Provide(
		NewCipher,
		NewPlatformRepo,
		NewAppRepo,
		fx.Annotate(NewOpenAPI, fx.As(new(wechatservice.OpenAPI))),
	),
)
