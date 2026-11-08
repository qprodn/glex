// Package wechatinfra hosts the wechat module's persistence adapters.
package wechatinfra

import "go.uber.org/fx"

// Module contains the wechat persistence adapters: the platform repo and
// the credential cipher (ADR 0029).
var Module = fx.Module("wechat-infra",
	fx.Provide(
		NewCipher,
		NewPlatformRepo,
	),
)
