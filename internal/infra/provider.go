package infra

import (
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	iaminfra "glex/internal/infra/iam"
	"glex/internal/infra/store"

	"github.com/google/wire"
)

// ProviderSet contains all infrastructure adapters used by the composition root.
var ProviderSet = wire.NewSet(store.ProviderSet, iaminfra.ProviderSet, accessinfra.ProviderSet, authninfra.ProviderSet)
