package application

import (
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	"glex/internal/application/iam"

	"github.com/google/wire"
)

// ProviderSet contains every application module exposed to the composition root.
var ProviderSet = wire.NewSet(iam.ProviderSet, accessapp.ProviderSet, authnapp.ProviderSet)
