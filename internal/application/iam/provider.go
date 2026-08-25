package iam

import "github.com/google/wire"

// ProviderSet contains the IAM application providers.
var ProviderSet = wire.NewSet(
	NewUserUsecase,
	NewMenuUsecase,
	NewRoleUsecase,
	NewRoleAssignmentUsecase,
	NewUserApplication,
	NewMenuApplication,
	NewRoleApplication,
	NewRoleAssignmentApplication,
)
