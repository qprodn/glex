package impl

import "go.uber.org/fx"

// Module contains the crossdomain authn contract adapters.
var Module = fx.Provide(
	NewSessionRevoker,
	NewCredentialWriter,
)
