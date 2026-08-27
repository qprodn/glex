package user

import "github.com/google/wire"

// ProviderSet contains the user module's application provider (System User
// profiles and their orchestration).
var ProviderSet = wire.NewSet(
	NewUserApplication,
)
