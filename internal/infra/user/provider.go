package userinfra

import "github.com/google/wire"

// ProviderSet contains the user module's persistence adapters.
var ProviderSet = wire.NewSet(
	NewUserRepo,
)
