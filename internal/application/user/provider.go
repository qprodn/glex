package user

import "go.uber.org/fx"

// Module contains the user module's application provider (System User
// profiles and their orchestration).
var Module = fx.Provide(
	NewUserApplication,
)
