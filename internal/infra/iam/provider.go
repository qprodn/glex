package iaminfra

import "github.com/google/wire"

// ProviderSet contains IAM persistence adapters.
var ProviderSet = wire.NewSet(
	NewUserRepo,
	NewMenuRepo,
	NewRoleRepo,
	NewUserRoleRepo,
)
