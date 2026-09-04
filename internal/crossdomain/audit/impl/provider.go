package impl

import "go.uber.org/fx"

// Module contains the audit crossdomain contract adapters.
var Module = fx.Module("audit-crossdomain",
	fx.Provide(
		NewLoginLogWriter,
	),
)
