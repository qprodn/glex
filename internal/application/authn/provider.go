package authn

import "go.uber.org/fx"

// Module contains the authn application providers.
var Module = fx.Module("authn-app",
	fx.Provide(
		NewLoginApplication,
		NewOnlineSessionApplication,
	),
)
