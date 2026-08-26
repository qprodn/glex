package authn

import "github.com/google/wire"

// ProviderSet contains the authn application providers.
var ProviderSet = wire.NewSet(
	NewLoginUsecase,
	NewLoginSessionUsecase,
	NewLoginApplication,
	NewOnlineSessionApplication,
)
