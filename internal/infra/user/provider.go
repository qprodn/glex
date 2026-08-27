package userinfra

import "go.uber.org/fx"

// Module contains the user module's persistence adapters.
var Module = fx.Provide(
	NewUserRepo,
)
