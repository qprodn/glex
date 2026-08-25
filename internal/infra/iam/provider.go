package iaminfra

import "github.com/google/wire"

// ProviderSet contains IAM persistence, cache, and authentication adapters.
var ProviderSet = wire.NewSet(
	NewUserRepo,
	NewMenuRepo,
	NewRoleRepo,
	NewUserRoleRepo,
	NewAuthStorage,
	NewAuthManager,
	NewLoginSessionRepo,
	NewAuthenticator,
	NewLoginCounterRepo,
	NewAuthLoginBackend,
)
