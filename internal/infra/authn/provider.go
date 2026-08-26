package authninfra

import "github.com/google/wire"

// ProviderSet contains authn persistence, cache, and authentication adapters.
var ProviderSet = wire.NewSet(
	NewAuthStorage,
	NewAuthManager,
	NewLoginSessionRepo,
	NewAuthenticator,
	NewLoginCounterRepo,
	NewAuthLoginBackend,
	NewCredentialRepo,
)
