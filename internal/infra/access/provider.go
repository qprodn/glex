package accessinfra

import "go.uber.org/fx"

// Module contains access persistence adapters.
var Module = fx.Module("access-infra",
	fx.Provide(
		NewMenuRepo,
		NewRoleRepo,
		NewUserRoleRepo,
		NewAuthorizationResolver,
	),
)
