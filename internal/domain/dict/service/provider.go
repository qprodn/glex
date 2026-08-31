package service

import "go.uber.org/fx"

// Module contains the dict domain service providers (ADR 0005).
var Module = fx.Module("dict-domain-service",
	fx.Provide(
		NewDictService,
	),
)
