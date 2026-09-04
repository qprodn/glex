package service

import "go.uber.org/fx"

// Module contains the org domain service providers (ADR 0005).
var Module = fx.Module("org-domain-service",
	fx.Provide(
		NewDeptService,
		NewPostService,
		NewPostAssignmentService,
	),
)
