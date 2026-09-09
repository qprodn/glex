package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"glex/internal/conf"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"
	_ "go.uber.org/automaxprocs"
	"go.uber.org/fx"
)

// go build -ldflags "-X main.Version=x.y.z"
var (
	// Name is the name of the compiled software.
	Name = "glex"
	// Version is the version of the compiled software.
	Version string
	// flagconf is the config flag.
	flagconf string

	id, _ = os.Hostname()
)

func init() {
	flag.StringVar(&flagconf, "conf", "../../configs", "config path, eg: -conf config.yaml")
}

func newApp(logger *slog.Logger, gs *grpc.Server, hs *http.Server) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(
			gs,
			hs,
		),
	)
}

func newLogger(handler slog.Handler) *slog.Logger {
	return log.NewLogger(
		handler,
		log.WithExtractor(tracing.TraceAttrs),
		log.WithFilter(log.FilterKey(
			"password",
			"token",
			"access_token",
			"bearer_token",
			"authorization",
		)),
	).With(
		slog.String("service.id", id),
		slog.String("service.name", Name),
		slog.String("service.version", Version),
	)
}

func main() {
	flag.Parse()
	logger := newLogger(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelInfo,
		}),
	)
	log.SetDefault(logger)
	c := config.New(
		config.WithSource(
			file.NewSource(flagconf),
		),
	)
	defer c.Close()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}

	// fx builds the graph and owns construction-time cleanup ordering; the
	// kratos App keeps owning the run loop, signal handling, and graceful
	// server shutdown. Lifecycle hooks run OnStop in LIFO order, so the
	// sa-token Manager closes before the shared Data connections.
	var app *kratos.App
	fxApp := fx.New(
		fx.Supply(bc.Server, bc.Data, bc.Auth, bc.File, bc.User, logger),
		appModule,
		fx.Populate(&app),
	)
	if err := fxApp.Err(); err != nil {
		panic(err)
	}
	ctx := context.Background()
	if err := fxApp.Start(ctx); err != nil {
		panic(err)
	}
	defer func() {
		if err := fxApp.Stop(ctx); err != nil {
			logger.Error("failed stopping dependency graph", slog.Any("error", err))
		}
	}()

	// start and wait for stop signal
	if err := app.Run(); err != nil {
		panic(err)
	}
}
