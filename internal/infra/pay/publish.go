package payinfra

import (
	"context"

	payservice "glex/internal/domain/pay/service"
	"glex/internal/eventbus"
)

// completedPublisher adapts the pay topic's eventbus.Producer onto the
// domain port. The domain module does not import eventbus (ADR 0027).
type completedPublisher struct {
	producer eventbus.Producer
}

// AdaptProducer binds a topic producer to the pay module's publish port.
// The composition root calls it; payinfra.Module does not provide it, so
// the eventbus wiring test can validate this module without a producer.
func AdaptProducer(producer eventbus.Producer) payservice.CompletedPublisher {
	return completedPublisher{producer: producer}
}

func (p completedPublisher) Publish(ctx context.Context, body []byte) error {
	return p.producer.Send(ctx, body)
}
