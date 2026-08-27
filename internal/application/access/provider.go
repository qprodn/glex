package access

import "go.uber.org/fx"

// Module contains the access application providers.
var Module = fx.Provide(
	NewMenuApplication,
	NewRoleApplication,
	NewRoleAssignmentApplication,
)
