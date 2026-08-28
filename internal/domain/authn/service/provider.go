package service

import "go.uber.org/fx"

// Module contains the authn domain service providers (ADR 0005).
var Module = fx.Module("authn-domain-service",
	fx.Provide(
		NewLoginService,
		NewOnlineSessionService,
		NewCredentialService,
	),
)
