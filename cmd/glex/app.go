package main

import (
	accessapp "glex/internal/application/access"
	auditapp "glex/internal/application/audit"
	authnapp "glex/internal/application/authn"
	dictapp "glex/internal/application/dict"
	fileapp "glex/internal/application/file"
	orgapp "glex/internal/application/org"
	payapp "glex/internal/application/pay"
	userapp "glex/internal/application/user"
	accessimpl "glex/internal/crossdomain/access/impl"
	auditimpl "glex/internal/crossdomain/audit/impl"
	authnimpl "glex/internal/crossdomain/authn/impl"
	dictimpl "glex/internal/crossdomain/dict/impl"
	fileimpl "glex/internal/crossdomain/file/impl"
	orgimpl "glex/internal/crossdomain/org/impl"
	payimpl "glex/internal/crossdomain/pay/impl"
	userimpl "glex/internal/crossdomain/user/impl"
	accessservice "glex/internal/domain/access/service"
	auditservice "glex/internal/domain/audit/service"
	authnservice "glex/internal/domain/authn/service"
	dictservice "glex/internal/domain/dict/service"
	fileservice "glex/internal/domain/file/service"
	jobservice "glex/internal/domain/job/service"
	orgservice "glex/internal/domain/org/service"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	userservice "glex/internal/domain/user/service"
	"glex/internal/eventbus"
	accessinfra "glex/internal/infra/access"
	auditinfra "glex/internal/infra/audit"
	authninfra "glex/internal/infra/authn"
	dictinfra "glex/internal/infra/dict"
	eventbusinfra "glex/internal/infra/eventbus"
	fileinfra "glex/internal/infra/file"
	jobinfra "glex/internal/infra/job"
	lockerinfra "glex/internal/infra/locker"
	orginfra "glex/internal/infra/org"
	payinfra "glex/internal/infra/pay"
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
// satisfies all of them (the file module declares no Transaction — its
// writes are single-row statements and its byte leg is non-transactional;
// audit joins none either — its rows are single-row records minted outside
// any business transaction, ADR 0012).
var appModule = fx.Options(
	// infrastructure
	store.Module,
	userinfra.Module,
	auditinfra.Module,
	accessinfra.Module,
	authninfra.Module,
	dictinfra.Module,
	fileinfra.Module,
	lockerinfra.Module,
	orginfra.Module,
	payinfra.Module,
	eventbusinfra.Module,
	jobinfra.Module,
	// domain services
	userservice.Module,
	auditservice.Module,
	accessservice.Module,
	authnservice.Module,
	dictservice.Module,
	fileservice.Module,
	orgservice.Module,
	payservice.Module,
	jobservice.Module,
	// crossdomain contract adapters
	auditimpl.Module,
	accessimpl.Module,
	authnimpl.Module,
	dictimpl.Module,
	fileimpl.Module,
	orgimpl.Module,
	payimpl.Module,
	userimpl.Module,
	// application
	userapp.Module,
	auditapp.Module,
	accessapp.Module,
	authnapp.Module,
	dictapp.Module,
	fileapp.Module,
	orgapp.Module,
	payapp.Module,
	// transport
	server.Module,
	fx.Provide(newApp),
	fx.Provide(
		func(m *store.TxManager) userapp.Transaction { return m },
		func(m *store.TxManager) accessapp.Transaction { return m },
		func(m *store.TxManager) authnapp.Transaction { return m },
		func(m *store.TxManager) dictapp.Transaction { return m },
		func(m *store.TxManager) orgapp.Transaction { return m },
		func(m *store.TxManager) payapp.Transaction { return m },
		// The notify route group's limiter, adapted to the server's
		// net/http filter shape (ADR 0020).
		func(l *payinfra.NotifyRateLimiter) server.NotifyLimiter { return l.Filter },
		// The pay module's event publisher: the bus producer bound to the
		// pay topic (ADR 0022).
		func(f eventbusinfra.ProducerFactory) eventbus.Producer {
			return f(payentity.TopicPayEvents)
		},
	),
)
