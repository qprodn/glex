package user

import "github.com/google/wire"

// ProviderSet contains the user module's application providers (System User
// profiles and their orchestration).
var ProviderSet = wire.NewSet(
	NewUserUsecase,
	NewUserApplication,
)
