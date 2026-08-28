package service

import "go.uber.org/fx"

// Module contains the access domain service providers (ADR 0005).
var Module = fx.Module("access-domain-service",
	fx.Provide(
		NewRoleService,
		NewMenuService,
		NewRoleAssignmentService,
		NewAuthorizationService,
	),
)
