package infra

import (
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	"glex/internal/infra/store"
	userinfra "glex/internal/infra/user"

	"github.com/google/wire"
)

// ProviderSet contains all infrastructure adapters used by the composition root.
var ProviderSet = wire.NewSet(store.ProviderSet, userinfra.ProviderSet, accessinfra.ProviderSet, authninfra.ProviderSet)
