//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.

package main

import (
	"log/slog"

	"glex/internal/application"
	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	userapp "glex/internal/application/user"
	"glex/internal/conf"
	accessimpl "glex/internal/crossdomain/access/impl"
	authnimpl "glex/internal/crossdomain/authn/impl"
	"glex/internal/infra"
	"glex/internal/infra/store"
	"glex/internal/server"

	"github.com/go-kratos/kratos/v3"
	"github.com/google/wire"
)

// wireApp init kratos application.
func wireApp(*conf.Server, *conf.Data, *conf.Auth, *slog.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		server.ProviderSet,
		infra.ProviderSet,
		accessimpl.ProviderSet,
		authnimpl.ProviderSet,
		wire.Bind(new(userapp.Transaction), new(*store.TxManager)),
		wire.Bind(new(accessapp.Transaction), new(*store.TxManager)),
		wire.Bind(new(authnapp.Transaction), new(*store.TxManager)),
		application.ProviderSet,
		newApp,
	))
}
