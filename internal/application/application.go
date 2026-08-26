package application

import (
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	userapp "glex/internal/application/user"

	"github.com/google/wire"
)

// ProviderSet contains every application module exposed to the composition root.
var ProviderSet = wire.NewSet(userapp.ProviderSet, accessapp.ProviderSet, authnapp.ProviderSet)
