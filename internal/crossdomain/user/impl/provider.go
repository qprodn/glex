package impl

import "go.uber.org/fx"

// Module contains the user crossdomain contract adapters.
var Module = fx.Module("user-crossdomain",
	fx.Provide(
		NewDeptUsageReader,
	),
)
