package service

import "go.uber.org/fx"

// Module contains the file domain service providers (ADR 0005).
var Module = fx.Module("file-domain-service",
	fx.Provide(
		NewFileService,
	),
)
