package service

import "go.uber.org/fx"

// Module contains the audit domain service providers (ADR 0005).
var Module = fx.Module("audit-domain-service",
	fx.Provide(
		NewAuditService,
	),
)
