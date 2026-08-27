package access

import "go.uber.org/fx"

// Module contains the access application providers.
var Module = fx.Module("access-app",
	fx.Provide(
		NewMenuApplication,
		NewRoleApplication,
		NewRoleAssignmentApplication,
	),
)
