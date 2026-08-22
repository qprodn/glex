package iam

import "github.com/google/wire"

// ProviderSet contains the IAM application providers.
var ProviderSet = wire.NewSet(
	NewUserUsecase,
	NewMenuUsecase,
	NewRoleUsecase,
	NewAuthUsecase,
	NewLoginSessionUsecase,
	NewUserApplication,
	NewMenuApplication,
	NewRoleApplication,
	NewAuthApplication,
	NewOnlineSessionApplication,
)
