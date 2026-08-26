package impl

import "github.com/google/wire"

// ProviderSet contains the crossdomain access contract adapters.
var ProviderSet = wire.NewSet(
	NewAuthorizationResolver,
)
