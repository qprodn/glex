package impl

import "github.com/google/wire"

// ProviderSet contains the crossdomain authn contract adapters.
var ProviderSet = wire.NewSet(
	NewSessionRevoker,
	NewCredentialWriter,
)
