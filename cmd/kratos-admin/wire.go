//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.

package main

import (
	"log/slog"

	"glex/internal/application"
	iamapp "glex/internal/application/iam"
	"glex/internal/conf"
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
		authnimpl.ProviderSet,
		wire.Bind(new(iamapp.Transaction), new(*store.TxManager)),
		application.ProviderSet,
		newApp,
	))
}
