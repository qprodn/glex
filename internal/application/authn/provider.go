package authn

import "go.uber.org/fx"

// Module contains the authn application providers.
var Module = fx.Provide(
	NewLoginApplication,
	NewOnlineSessionApplication,
)
