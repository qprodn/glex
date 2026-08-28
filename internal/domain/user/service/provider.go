package service

import "go.uber.org/fx"

// Module contains the System User domain service providers (ADR 0005).
var Module = fx.Module("user-domain-service",
	fx.Provide(NewUserService),
)
