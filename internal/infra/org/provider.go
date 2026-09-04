package orginfra

import "go.uber.org/fx"

// Module contains org persistence adapters.
var Module = fx.Module("org-infra",
	fx.Provide(
		NewDeptRepo,
	),
)
