package iaminfra

import "github.com/google/wire"

// ProviderSet contains IAM persistence adapters (the remaining System User
// span; role, menu, and assignment storage moved to the access module).
var ProviderSet = wire.NewSet(
	NewUserRepo,
)
