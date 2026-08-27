package impl

import "go.uber.org/fx"

// Module contains the crossdomain access contract adapters.
var Module = fx.Module("access-crossdomain",
	fx.Provide(NewAuthorizationResolver),
)
