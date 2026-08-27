package application

import (
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	userapp "glex/internal/application/user"

	"go.uber.org/fx"
)

// Module contains every application module exposed to the composition root.
var Module = fx.Options(userapp.Module, accessapp.Module, authnapp.Module)
