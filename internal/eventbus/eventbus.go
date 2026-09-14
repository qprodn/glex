// Package eventbus defines the in-process event contracts (ADR 0022),
// copied verbatim in shape from coze-studio's backend/infra/eventbus: a
// producer bound to one topic, a consumer service registering one handler
// per (topic, consumer group), and a byte-body message with no transport
// envelope — the JSON body is the payload. Application modules depend on
// this package only (the internal/lock pattern); implementations live in
// internal/infra/eventbus/impl/<backend> and are bound via fx.
package eventbus

import "context"

// Message is one delivered event body. Body is raw bytes; serialization is
// the caller's concern — the JSON shape is the cross-module contract
// (ADR 0022).
type Message struct {
	Topic string
	Group string
	Body  []byte
}

// ConsumerHandler processes one delivered message. Returning an error
// means redelivery (at-least-once within a live process); a handler that
// can never succeed still exhausts its retry budget and is dead-lettered
// by the backend, so handlers need no poison-message guard of their own.
type ConsumerHandler interface {
	HandleMessage(ctx context.Context, msg *Message) error
}

// Producer publishes event bodies to the topic it was constructed for
// (coze-studio binds one producer per topic). Send never blocks on slow
// consumers: a full consumer-group queue fails loudly instead.
type Producer interface {
	Send(ctx context.Context, body []byte, opts ...SendOpt) error
	BatchSend(ctx context.Context, bodyArr [][]byte, opts ...SendOpt) error
}

// ConsumerService registers a handler as one consumer group on a topic.
// nameServer is the backend's address ("" — ignored by the in-memory
// backend); it exists to keep the coze-studio signature so a broker
// backend later is a drop-in.
type ConsumerService interface {
	RegisterConsumer(nameServer, topic, group string, consumerHandler ConsumerHandler, opts ...ConsumerOpt) error
}

// Subscription is glex's fx wiring shape (not part of the coze-studio
// contract): each module provides its subscriptions into the
// "eventbus-subscriptions" value group and the eventbus module registers
// them all at the one aggregation point — no package-level service
// locator (ADR 0022).
type Subscription struct {
	NameServer string
	Topic      string
	Group      string
	Handler    ConsumerHandler
}
