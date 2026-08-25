package accessinfra

import "github.com/google/wire"

// ProviderSet contains access persistence adapters.
var ProviderSet = wire.NewSet(
	NewMenuRepo,
	NewRoleRepo,
	NewUserRoleRepo,
	NewAuthorizationResolver,
)
