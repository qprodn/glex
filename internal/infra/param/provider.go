package paraminfra

import "go.uber.org/fx"

// Module contains param persistence adapters.
var Module = fx.Module("param-infra",
	fx.Provide(
		NewConfigRepo,
	),
)
