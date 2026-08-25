package infra

import (
	authninfra "glex/internal/infra/authn"
	iaminfra "glex/internal/infra/iam"
	"glex/internal/infra/store"

	"github.com/google/wire"
)

// ProviderSet contains all infrastructure adapters used by the composition root.
var ProviderSet = wire.NewSet(store.ProviderSet, iaminfra.ProviderSet, authninfra.ProviderSet)
