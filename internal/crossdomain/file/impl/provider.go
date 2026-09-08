package impl

import "go.uber.org/fx"

// Module contains the file crossdomain contract adapters.
var Module = fx.Module("file-crossdomain",
	fx.Provide(
		NewURLResolver,
	),
)
