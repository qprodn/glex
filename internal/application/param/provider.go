package param

import "go.uber.org/fx"

// Module contains the param application providers.
var Module = fx.Module("param-app",
	fx.Provide(
		NewConfigApplication,
	),
)
