package main

import (
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	userapp "glex/internal/application/user"
	accessimpl "glex/internal/crossdomain/access/impl"
	authnimpl "glex/internal/crossdomain/authn/impl"
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	lockerinfra "glex/internal/infra/locker"
	"glex/internal/infra/store"
	userinfra "glex/internal/infra/user"
	"glex/internal/server"

	"go.uber.org/fx"
)

// appModule assembles the whole dependency graph. The Transaction adapters
// keep the dependency rule: each application module consumes its own
// interface, and only the composition root knows that *store.TxManager
// satisfies all three.
var appModule = fx.Options(
	// infrastructure
	store.Module,
	userinfra.Module,
	accessinfra.Module,
	authninfra.Module,
	lockerinfra.Module,
	// crossdomain contract adapters
	accessimpl.Module,
	authnimpl.Module,
	// application
	userapp.Module,
	accessapp.Module,
	authnapp.Module,
	// transport
	server.Module,
	fx.Provide(newApp),
	fx.Provide(
		func(m *store.TxManager) userapp.Transaction { return m },
		func(m *store.TxManager) accessapp.Transaction { return m },
		func(m *store.TxManager) authnapp.Transaction { return m },
	),
)
