package file

import "go.uber.org/fx"

// Module contains the file application providers.
var Module = fx.Module("file-app",
	fx.Provide(
		NewFileApplication,
	),
)
