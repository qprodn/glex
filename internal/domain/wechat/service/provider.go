package service

import "go.uber.org/fx"

// Module contains the wechat domain service providers (ADR 0005).
var Module = fx.Module("wechat-domain-service",
	fx.Provide(
		NewPushAuthenticator,
		NewAppService,
		func(apps App) PlatformBindings { return apps },
		NewPlatformService,
	),
)
