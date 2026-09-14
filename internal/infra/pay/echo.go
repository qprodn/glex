package payinfra

import (
	"context"
	"log/slog"

	payentity "glex/internal/domain/pay/entity"
	"glex/internal/eventbus"
)

// ConsumeGroupEcho names the pay module's standing diagnostic consumer
// group on its own topic — the subscriber owns its group name (ADR 0022).
const ConsumeGroupEcho = "echo"

// NewEchoSubscription provides the module's standing diagnostic
// subscription into the eventbus-subscriptions value group: one echo
// group that logs every pay event, proving the pipeline stays alive in
// production. It is the tracer bullet's example subscriber; the first
// real business subscriber replaces it (ADR 0022). A module with several
// subscriptions provides one such constructor per subscription.
func NewEchoSubscription(logger *slog.Logger) eventbus.Subscription {
	return eventbus.Subscription{
		Topic:   payentity.TopicPayEvents,
		Group:   ConsumeGroupEcho,
		Handler: echoHandler{logger: logger.With(slog.String("component", "infra.pay.echo"))},
	}
}

type echoHandler struct {
	logger *slog.Logger
}

// HandleMessage logs the delivery's stable discriminators at Info and the
// payload at Debug only — payloads carry channel identifiers (payer_id)
// that have no business in production Info logs.
func (h echoHandler) HandleMessage(_ context.Context, msg *eventbus.Message) error {
	h.logger.Info("event received",
		slog.String("operation", "pay.echo"),
		slog.String("topic", msg.Topic),
		slog.String("group", msg.Group),
		slog.Int("payload_bytes", len(msg.Body)))
	h.logger.Debug("event payload",
		slog.String("operation", "pay.echo"),
		slog.String("payload", string(msg.Body)))
	return nil
}
