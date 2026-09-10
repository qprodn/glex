package pay

import "go.uber.org/fx"

// Module contains the pay application use cases.
var Module = fx.Module("pay-app",
	fx.Provide(NewPayApplication),
)
