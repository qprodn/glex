package auditinfra

import "go.uber.org/fx"

// Module contains audit persistence adapters.
var Module = fx.Module("audit-infra",
	fx.Provide(
		NewOperLogRepo,
		NewLoginLogRepo,
	),
)
