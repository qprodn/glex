package impl

import "go.uber.org/fx"

// Module contains the org crossdomain contract adapters.
var Module = fx.Module("org-crossdomain",
	fx.Provide(
		NewDeptReader,
	),
)
