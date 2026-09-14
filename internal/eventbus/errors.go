package eventbus

import "errors"

// Sentinel errors callers switch on. Publishing is best-effort (ADR 0022),
// so these surface as Warn breadcrumbs at the publishing site, never as
// user-facing failures.
var (
	// ErrQueueFull: a consumer group's bounded queue is full — the group is
	// stalled or the burst exceeded capacity. The event was not enqueued to
	// that group (other groups may already hold their copy).
	ErrQueueFull = errors.New("eventbus: consumer group queue full")

	// ErrBusClosed: the bus has stopped; sends and registrations fail.
	ErrBusClosed = errors.New("eventbus: bus closed")

	// ErrConsumerGroupRegistered: the (topic, group) pair is already
	// registered.
	ErrConsumerGroupRegistered = errors.New("eventbus: consumer group already registered")

	// ErrBusStarted: registration is wiring-time only; the bus is already
	// dispatching.
	ErrBusStarted = errors.New("eventbus: bus started, registration is wiring-time only")
)
