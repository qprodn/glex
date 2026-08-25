package iam

import "github.com/google/wire"

// ProviderSet contains the IAM application providers (the remaining System
// User span; roles, menus, and assignments moved to the access module).
var ProviderSet = wire.NewSet(
	NewUserUsecase,
	NewUserApplication,
)
