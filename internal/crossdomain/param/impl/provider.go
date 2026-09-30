package impl

import "go.uber.org/fx"

// Module contains the param crossdomain contract adapters.
var Module = fx.Module("param-crossdomain",
	fx.Provide(
		NewConfigReader,
	),
)
