package dictinfra

import "go.uber.org/fx"

// Module contains dict persistence adapters.
var Module = fx.Module("dict-infra",
	fx.Provide(
		NewDictTypeRepo,
		NewDictEntryRepo,
	),
)
