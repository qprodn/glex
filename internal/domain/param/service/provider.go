package service

import "go.uber.org/fx"

// Module contains the param domain service providers (ADR 0005).
var Module = fx.Module("param-domain-service",
	fx.Provide(
		NewConfigsService,
	),
)
