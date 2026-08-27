package authn

import "github.com/google/wire"

// ProviderSet contains the authn application providers.
var ProviderSet = wire.NewSet(
	NewLoginApplication,
	NewOnlineSessionApplication,
)
