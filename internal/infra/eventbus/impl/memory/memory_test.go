package memory

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"glex/internal/eventbus"
)

// logCapture collects records so the dead-letter and retry breadcrumbs can
// be asserted (ADR 0011's field rules apply to the bus's own logs).
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *logCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *logCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logCapture) WithGroup(string) slog.Handler      { return h }

func (h *logCapture) count(level slog.Level, msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level && r.Message == msg {
			n++
		}
	}
	return n
}

// handlerFake records deliveries; err (when set) fails every attempt,
// panics fails the first N attempts with a panic, gate blocks each call
// until its channel (or the context) closes.
type handlerFake struct {
	received chan []byte
	calls    atomic.Int32
	err      error
	panics   int
	gate     chan struct{}
}

func newHandlerFake(capacity int) *handlerFake {
	return &handlerFake{received: make(chan []byte, capacity)}
}

func (h *handlerFake) HandleMessage(ctx context.Context, msg *eventbus.Message) error {
	n := h.calls.Add(1)
	if h.panics > 0 && int(n) <= h.panics {
		panic("handler exploded")
	}
	if h.gate != nil {
		select {
		case <-h.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if h.err != nil {
		return h.err
	}
	h.received <- msg.Body
	return nil
}

func newTestBus(t *testing.T, opts ...Option) (*Bus, *logCapture) {
	t.Helper()
	logs := &logCapture{}
	bus := NewBus(slog.New(logs), opts...)
	return bus, logs
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func mustRegister(t *testing.T, bus *Bus, topic, group string, h eventbus.ConsumerHandler) {
	t.Helper()
	if err := bus.RegisterConsumer("", topic, group, h); err != nil {
		t.Fatalf("RegisterConsumer(%s, %s) = %v", topic, group, err)
	}
}

func TestSendFansOutToEveryGroupInOrder(t *testing.T) {
	bus, _ := newTestBus(t, WithQueueSize(8), WithRetryBackoff([]time.Duration{time.Millisecond}))
	g1, g2 := newHandlerFake(8), newHandlerFake(8)
	mustRegister(t, bus, "pay.events", "echo", g1)
	mustRegister(t, bus, "pay.events", "audit", g2)
	bus.Start()
	defer bus.Stop()

	p := bus.Producer("pay.events")
	for _, body := range []string{"a", "b", "c"} {
		if err := p.Send(context.Background(), []byte(body)); err != nil {
			t.Fatalf("Send(%s) = %v", body, err)
		}
	}
	for _, h := range []*handlerFake{g1, g2} {
		for _, want := range []string{"a", "b", "c"} {
			select {
			case got := <-h.received:
				if string(got) != want {
					t.Fatalf("group received %q, want %q", got, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for delivery")
			}
		}
	}
}

func TestSendToTopicWithoutGroupsIsNoop(t *testing.T) {
	bus, _ := newTestBus(t)
	bus.Start()
	defer bus.Stop()
	if err := bus.Producer("nobody.listens").Send(context.Background(), []byte("x")); err != nil {
		t.Fatalf("Send to groupless topic = %v, want nil", err)
	}
}

func TestFullQueueFailsSendLoudly(t *testing.T) {
	bus, _ := newTestBus(t, WithQueueSize(1))
	mustRegister(t, bus, "pay.events", "echo", newHandlerFake(1))
	// Not started: nothing drains, the queue holds exactly one.
	p := bus.Producer("pay.events")
	if err := p.Send(context.Background(), []byte("first")); err != nil {
		t.Fatalf("first Send = %v", err)
	}
	err := p.Send(context.Background(), []byte("second"))
	if !errors.Is(err, eventbus.ErrQueueFull) {
		t.Fatalf("second Send = %v, want ErrQueueFull", err)
	}
}

func TestRetryThenDeadLetterLogsErrorOnce(t *testing.T) {
	bus, logs := newTestBus(t, WithQueueSize(4), WithRetryBackoff([]time.Duration{time.Millisecond, time.Millisecond}))
	h := newHandlerFake(4)
	h.err = errors.New("always broken")
	mustRegister(t, bus, "pay.events", "echo", h)
	bus.Start()
	defer bus.Stop()

	if err := bus.Producer("pay.events").Send(context.Background(), []byte(`{"type":"payment.completed"}`)); err != nil {
		t.Fatalf("Send = %v", err)
	}
	waitFor(t, func() bool { return h.calls.Load() == 3 }) // initial attempt + 2 retries
	time.Sleep(50 * time.Millisecond)                      // no further attempts, no further logs
	if h.calls.Load() != 3 {
		t.Fatalf("handler calls = %d, want 3", h.calls.Load())
	}
	if got := logs.count(slog.LevelError, "eventbus delivery abandoned — dead letter"); got != 1 {
		t.Fatalf("dead-letter Error logs = %d, want 1", got)
	}
	if got := logs.count(slog.LevelWarn, "eventbus delivery failed, retrying"); got != 2 {
		t.Fatalf("retry Warn logs = %d, want 2", got)
	}
}

func TestPanicCountsAsFailedAttempt(t *testing.T) {
	bus, _ := newTestBus(t, WithQueueSize(4), WithRetryBackoff([]time.Duration{time.Millisecond, time.Millisecond}))
	h := newHandlerFake(4)
	h.panics = 2 // fails twice, succeeds on the third attempt
	mustRegister(t, bus, "pay.events", "echo", h)
	bus.Start()
	defer bus.Stop()

	if err := bus.Producer("pay.events").Send(context.Background(), []byte("survivor")); err != nil {
		t.Fatalf("Send = %v", err)
	}
	select {
	case got := <-h.received:
		if string(got) != "survivor" {
			t.Fatalf("received %q, want %q", got, "survivor")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panicking handler killed the dispatcher: delivery never recovered")
	}
}

func TestStopDrainsInFlightAndDropsQueued(t *testing.T) {
	bus, logs := newTestBus(t, WithQueueSize(8), WithRetryBackoff([]time.Duration{time.Hour}))
	h := newHandlerFake(8)
	h.gate = make(chan struct{})
	mustRegister(t, bus, "pay.events", "echo", h)
	bus.Start()

	if err := bus.Producer("pay.events").Send(context.Background(), []byte("in-flight")); err != nil {
		t.Fatalf("first Send = %v", err)
	}
	// First delivery is inside the handler; two more sit queued.
	<-time.After(20 * time.Millisecond)
	if err := bus.Producer("pay.events").Send(context.Background(), []byte("queued-1")); err != nil {
		t.Fatalf("second Send = %v", err)
	}
	if err := bus.Producer("pay.events").Send(context.Background(), []byte("queued-2")); err != nil {
		t.Fatalf("third Send = %v", err)
	}
	waitFor(t, func() bool { return h.calls.Load() >= 1 })

	bus.Stop() // cancels the in-flight handler's context, drops the queued pair
	if h.calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1 (queued events must die with the stop)", h.calls.Load())
	}
	if got := logs.count(slog.LevelError, "eventbus delivery abandoned — dead letter"); got != 0 {
		t.Fatalf("drain produced %d dead-letter logs, want 0", got)
	}
	err := bus.Producer("pay.events").Send(context.Background(), []byte("late"))
	if !errors.Is(err, eventbus.ErrBusClosed) {
		t.Fatalf("Send after Stop = %v, want ErrBusClosed", err)
	}
}

func TestDuplicateGroupRegistrationRejected(t *testing.T) {
	bus, _ := newTestBus(t)
	mustRegister(t, bus, "pay.events", "echo", newHandlerFake(1))
	err := bus.RegisterConsumer("", "pay.events", "echo", newHandlerFake(1))
	if !errors.Is(err, eventbus.ErrConsumerGroupRegistered) {
		t.Fatalf("duplicate RegisterConsumer = %v, want ErrConsumerGroupRegistered", err)
	}
	// A different group on the same topic is a second subscription, not a
	// duplicate.
	mustRegister(t, bus, "pay.events", "audit", newHandlerFake(1))
}

func TestRegistrationAfterStartRejected(t *testing.T) {
	bus, _ := newTestBus(t)
	bus.Start()
	defer bus.Stop()
	err := bus.RegisterConsumer("", "pay.events", "echo", newHandlerFake(1))
	if !errors.Is(err, eventbus.ErrBusStarted) {
		t.Fatalf("RegisterConsumer after Start = %v, want ErrBusStarted", err)
	}
}

func TestBatchSendDeliversInOrder(t *testing.T) {
	bus, _ := newTestBus(t, WithQueueSize(8))
	h := newHandlerFake(8)
	mustRegister(t, bus, "pay.events", "echo", h)
	bus.Start()
	defer bus.Stop()

	bodies := [][]byte{[]byte("x"), []byte("y"), []byte("z")}
	if err := bus.Producer("pay.events").BatchSend(context.Background(), bodies); err != nil {
		t.Fatalf("BatchSend = %v", err)
	}
	for _, want := range []string{"x", "y", "z"} {
		select {
		case got := <-h.received:
			if string(got) != want {
				t.Fatalf("received %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for delivery")
		}
	}
}
