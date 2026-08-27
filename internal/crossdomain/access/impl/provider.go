package impl

import "go.uber.org/fx"

// Module contains the crossdomain access contract adapters.
var Module = fx.Provide(
	NewAuthorizationResolver,
)
