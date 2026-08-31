package main

import (
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	dictapp "glex/internal/application/dict"
	userapp "glex/internal/application/user"
	accessimpl "glex/internal/crossdomain/access/impl"
	authnimpl "glex/internal/crossdomain/authn/impl"
	accessservice "glex/internal/domain/access/service"
	authnservice "glex/internal/domain/authn/service"
	dictservice "glex/internal/domain/dict/service"
	userservice "glex/internal/domain/user/service"
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	dictinfra "glex/internal/infra/dict"
	lockerinfra "glex/internal/infra/locker"
	"glex/internal/infra/store"
	userinfra "glex/internal/infra/user"
	"glex/internal/server"

	"go.uber.org/fx"
)

// appModule assembles the whole dependency graph (ADR 0005: infra provides
// the repo ports, the domain services are the ports' only consumers, and the
// application use cases consume the domain services). The Transaction
// adapters keep the dependency rule: each application module consumes its own
// interface, and only the composition root knows that *store.TxManager
// satisfies all three.
var appModule = fx.Options(
	// infrastructure
	store.Module,
	userinfra.Module,
	accessinfra.Module,
	authninfra.Module,
	dictinfra.Module,
	lockerinfra.Module,
	// domain services
	userservice.Module,
	accessservice.Module,
	authnservice.Module,
	dictservice.Module,
	// crossdomain contract adapters
	accessimpl.Module,
	authnimpl.Module,
	// application
	userapp.Module,
	accessapp.Module,
	authnapp.Module,
	dictapp.Module,
	// transport
	server.Module,
	fx.Provide(newApp),
	fx.Provide(
		func(m *store.TxManager) userapp.Transaction { return m },
		func(m *store.TxManager) accessapp.Transaction { return m },
		func(m *store.TxManager) authnapp.Transaction { return m },
		func(m *store.TxManager) dictapp.Transaction { return m },
	),
)
