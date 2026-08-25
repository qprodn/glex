package access

import "github.com/google/wire"

// ProviderSet contains the access application providers.
var ProviderSet = wire.NewSet(
	NewMenuUsecase,
	NewRoleUsecase,
	NewRoleAssignmentUsecase,
	NewMenuApplication,
	NewRoleApplication,
	NewRoleAssignmentApplication,
)
