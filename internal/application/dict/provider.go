package dict

import "go.uber.org/fx"

// Module contains the dict application providers.
var Module = fx.Module("dict-app",
	fx.Provide(
		NewDictApplication,
	),
)
