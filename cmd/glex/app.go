package main

import (
	"glex/internal/application"
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	userapp "glex/internal/application/user"
	accessimpl "glex/internal/crossdomain/access/impl"
	authnimpl "glex/internal/crossdomain/authn/impl"
	"glex/internal/infra"
	"glex/internal/infra/store"
	"glex/internal/server"

	"go.uber.org/fx"
)

// appModule assembles the whole dependency graph. The Transaction adapters
// keep the dependency rule: each application module consumes its own
// interface, and only the composition root knows that *store.TxManager
// satisfies all three.
var appModule = fx.Options(
	infra.Module,
	accessimpl.Module,
	authnimpl.Module,
	application.Module,
	server.Module,
	fx.Provide(newApp),
	fx.Provide(
		func(m *store.TxManager) userapp.Transaction { return m },
		func(m *store.TxManager) accessapp.Transaction { return m },
		func(m *store.TxManager) authnapp.Transaction { return m },
	),
)
