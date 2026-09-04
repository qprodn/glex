package audit

import "go.uber.org/fx"

// Module contains the audit application providers.
var Module = fx.Module("audit-app",
	fx.Provide(
		NewAuditApplication,
	),
)
