package store

import (
	"log/slog"

	"glex/internal/conf"

	"go.uber.org/fx"
)

// Module contains shared PostgreSQL, Redis, and transaction providers.
var Module = fx.Options(
	fx.Provide(provideData, NewTxManager),
)

// provideData registers NewData's cleanup as a lifecycle hook so the ent
// client and the shared Redis connection close on shutdown.
func provideData(lc fx.Lifecycle, c *conf.Data, logger *slog.Logger) (*Data, error) {
	data, cleanup, err := NewData(c, logger)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.StopHook(cleanup))
	return data, nil
}
