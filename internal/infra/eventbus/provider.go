// Package eventbusinfra wires the eventbus (ADR 0022): the in-memory Bus
// under fx lifecycle, the ConsumerService binding, the producer factory
// for the composition root, and the one invoke that registers every
// module's subscription list — no package-level singleton (unlike
// coze-studio).
package eventbusinfra

import (
	"context"
	"fmt"
	"log/slog"

	"glex/internal/eventbus"
	"glex/internal/infra/eventbus/impl/memory"

	"go.uber.org/fx"
)

// Module contains the eventbus infra: the bus, its ConsumerService
// binding, the producer factory, and the subscription registration
// invoke. Subscription lists themselves are provided by the subscribing
// modules into the "eventbus-subscriptions" value group — the invoke's
// ParamTags and the providers' ResultTags must name the same group, which
// the wiring test proves with the real modules.
var Module = fx.Module("eventbus-infra",
	fx.Provide(
		NewBus,
		func(b *memory.Bus) eventbus.ConsumerService { return b },
		NewProducerFactory,
	),
	fx.Invoke(fx.Annotate(RegisterSubscriptions, fx.ParamTags(`group:"eventbus-subscriptions"`))),
)

// NewBus builds the bus and owns its lifecycle: the dispatch goroutines
// start on the fx start hook and Stop drains in-flight handlers — the
// repo's first resident background goroutines (ADR 0022).
func NewBus(lc fx.Lifecycle, logger *slog.Logger) *memory.Bus {
	bus := memory.NewBus(logger)
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error { bus.Start(); return nil },
		OnStop:  func(context.Context) error { bus.Stop(); return nil },
	})
	return bus
}

// ProducerFactory mints topic-bound producers. The composition root uses
// it to bind each module's eventbus.Producer without importing the memory
// backend — when a broker backend arrives, only this factory changes
// (ADR 0022).
type ProducerFactory func(topic string) eventbus.Producer

// NewProducerFactory adapts the bus.
func NewProducerFactory(b *memory.Bus) ProducerFactory { return b.Producer }

// RegisterSubscriptions registers every provided subscription — the one
// aggregation point (the composition-root rule, ADR 0022). subs comes
// first because the group ParamTags bind positionally.
func RegisterSubscriptions(subs []eventbus.Subscription, svc eventbus.ConsumerService) error {
	for _, s := range subs {
		if err := svc.RegisterConsumer(s.NameServer, s.Topic, s.Group, s.Handler); err != nil {
			return fmt.Errorf("eventbus: register %s/%s: %w", s.Topic, s.Group, err)
		}
	}
	return nil
}
