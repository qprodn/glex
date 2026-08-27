package infra

import (
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	lockerinfra "glex/internal/infra/locker"
	"glex/internal/infra/store"
	userinfra "glex/internal/infra/user"

	"go.uber.org/fx"
)

// Module contains all infrastructure adapters used by the composition root.
var Module = fx.Options(store.Module, userinfra.Module, accessinfra.Module, authninfra.Module, lockerinfra.Module)
