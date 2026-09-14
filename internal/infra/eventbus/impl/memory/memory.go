// Package memory implements the eventbus contracts over in-process queues
// (ADR 0022): one bounded FIFO channel per registered consumer group, one
// dispatch goroutine per group, in-process retries on a fixed backoff
// schedule, and a dead-letter Error log. Delivery is best-effort — a stop
// drops queued events; within a live process it is at-least-once, so
// handlers must be idempotent.
package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"glex/internal/eventbus"
)

// defaultQueueSize is each group's bounded queue capacity; the loud-failure
// posture (ADR 0022): saturation surfaces as ErrQueueFull at the publisher.
const defaultQueueSize = 1024

// defaultRetryBackoff follows the initial attempt's failure with three
// retries (ADR 0022).
var defaultRetryBackoff = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// Option customises the bus (tests inject fast schedules and small
// queues; production runs the defaults).
type Option func(*config)

type config struct {
	queueSize    int
	retryBackoff []time.Duration
}

// WithQueueSize overrides the per-group queue capacity.
func WithQueueSize(n int) Option { return func(c *config) { c.queueSize = n } }

// WithRetryBackoff overrides the retry schedule; the schedule's length is
// the retry count after the initial attempt.
func WithRetryBackoff(backoff []time.Duration) Option {
	return func(c *config) { c.retryBackoff = backoff }
}

// Bus is the in-memory eventbus: the coze-studio contracts over per-group
// queues. All exported methods are safe for concurrent use.
type Bus struct {
	logger *slog.Logger
	cfg    config

	mu      sync.Mutex
	groups  map[string]*consumer // key: topic + "/" + group
	started bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
}

type consumer struct {
	topic   string
	group   string
	handler eventbus.ConsumerHandler
	queue   chan []byte
}

// NewBus creates the bus. Dispatch goroutines start at Start and drain at
// Stop (the fx lifecycle hooks, ADR 0022).
func NewBus(logger *slog.Logger, opts ...Option) *Bus {
	cfg := config{queueSize: defaultQueueSize, retryBackoff: defaultRetryBackoff}
	for _, o := range opts {
		o(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Bus{
		logger: logger.With(slog.String("component", "infra.eventbus")),
		cfg:    cfg,
		groups: map[string]*consumer{},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Producer returns the topic-bound producer (coze-studio: one producer per
// topic).
func (b *Bus) Producer(topic string) eventbus.Producer { return &producer{bus: b, topic: topic} }

// Start spawns the dispatch goroutine for every registered group.
func (b *Bus) Start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return
	}
	b.started = true
	for _, c := range b.groups {
		b.spawnLocked(c)
	}
}

// Stop cancels dispatching and waits for in-flight handlers; queued events
// are dropped (best-effort delivery, ADR 0022). Safe to call twice.
func (b *Bus) Stop() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}
	b.cancel()
	b.wg.Wait()
}

// RegisterConsumer implements eventbus.ConsumerService. Registration is
// wiring-time only — after Start it fails with ErrBusStarted.
func (b *Bus) RegisterConsumer(nameServer, topic, group string, consumerHandler eventbus.ConsumerHandler, opts ...eventbus.ConsumerOpt) error {
	// nameServer and opts exist to keep the coze-studio signature; the
	// in-memory backend has no name server and is ordered by construction.
	if consumerHandler == nil {
		return errors.New("eventbus: nil consumer handler")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed.Load() {
		return eventbus.ErrBusClosed
	}
	if b.started {
		return eventbus.ErrBusStarted
	}
	key := topic + "/" + group
	if _, dup := b.groups[key]; dup {
		return fmt.Errorf("%w: %s", eventbus.ErrConsumerGroupRegistered, key)
	}
	b.groups[key] = &consumer{
		topic:   topic,
		group:   group,
		handler: consumerHandler,
		queue:   make(chan []byte, b.cfg.queueSize),
	}
	return nil
}

func (b *Bus) spawnLocked(c *consumer) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			// Priority check: once draining, a ready queue must never win
			// the select against a cancelled context (Go picks randomly
			// between ready cases).
			select {
			case <-b.ctx.Done():
				return
			default:
			}
			select {
			case <-b.ctx.Done():
				return
			case body := <-c.queue:
				b.deliver(c, body)
			}
		}
	}()
}

// deliver runs one delivery through its retry schedule; an exhausted
// budget ends in exactly one Error log carrying the full payload — that
// log line is the dead letter (ADR 0007's handling site, ADR 0022).
func (b *Bus) deliver(c *consumer, body []byte) {
	msg := &eventbus.Message{Topic: c.topic, Group: c.group, Body: body}
	for attempt := 0; ; attempt++ {
		err := b.invoke(c.handler, msg)
		if err == nil {
			return
		}
		if b.ctx.Err() != nil {
			// Draining: the in-flight failure gets no retry and no dead
			// letter (context.Canceled is exempt everywhere).
			return
		}
		if attempt >= len(b.cfg.retryBackoff) {
			b.logger.Error("eventbus delivery abandoned — dead letter",
				slog.String("operation", "eventbus.deliver"),
				slog.String("topic", c.topic),
				slog.String("group", c.group),
				slog.Int("attempts", attempt+1),
				slog.Any("error", err),
				slog.String("payload", string(body)))
			return
		}
		b.logger.Warn("eventbus delivery failed, retrying",
			slog.String("operation", "eventbus.deliver"),
			slog.String("topic", c.topic),
			slog.String("group", c.group),
			slog.Int("attempt", attempt+1),
			slog.Any("error", err))
		select {
		case <-b.ctx.Done():
			return
		case <-time.After(b.cfg.retryBackoff[attempt]):
		}
	}
}

// invoke runs the handler with panic isolation — a panic counts as one
// failed attempt, never a crashed dispatcher.
func (b *Bus) invoke(handler eventbus.ConsumerHandler, msg *eventbus.Message) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return handler.HandleMessage(b.ctx, msg)
}

type producer struct {
	bus   *Bus
	topic string
}

// Send enqueues the body to every group registered on the topic. It never
// blocks: a full group queue fails with ErrQueueFull (the loud failure,
// ADR 0022) while groups enqueued before the failure keep their copy. A
// topic with no groups is a no-op success — publishers do not know their
// subscribers.
func (p *producer) Send(ctx context.Context, body []byte, opts ...eventbus.SendOpt) error {
	return p.bus.enqueue(p.topic, body)
}

// BatchSend sends each body in order and stops at the first failure
// (earlier bodies may already be enqueued — best-effort).
func (p *producer) BatchSend(ctx context.Context, bodyArr [][]byte, opts ...eventbus.SendOpt) error {
	for _, body := range bodyArr {
		if err := p.Send(ctx, body, opts...); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bus) enqueue(topic string, body []byte) error {
	if b.closed.Load() {
		return eventbus.ErrBusClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var full []string
	for _, c := range b.groups {
		if c.topic != topic {
			continue
		}
		select {
		case c.queue <- body:
		default:
			full = append(full, c.group)
		}
	}
	if len(full) > 0 {
		return fmt.Errorf("%w: topic %s groups %v", eventbus.ErrQueueFull, topic, full)
	}
	return nil
}
