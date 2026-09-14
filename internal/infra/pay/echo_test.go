package payinfra

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	payentity "glex/internal/domain/pay/entity"
	eventbusinfra "glex/internal/infra/eventbus"
	"glex/internal/infra/eventbus/impl/memory"
)

// logCapture collects records so the echo subscriber's output can be
// asserted.
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

func (h *logCapture) find(level slog.Level, msg string) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == level && r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

// The tracer bullet's end-to-end path (ADR 0022): a real
// PaymentCompletedEvent's JSON goes through the real Bus to the echo
// subscription, registered the way production registers it — via the
// aggregation invoke, from the module's own subscription list.
func TestEchoSubscriptionConsumesPaymentCompletedEndToEnd(t *testing.T) {
	logs := &logCapture{}
	bus := memory.NewBus(slog.New(logs), memory.WithQueueSize(4))
	if err := eventbusinfra.RegisterSubscriptions(bus, NewEchoSubscriptions(slog.New(logs))); err != nil {
		t.Fatalf("RegisterSubscriptions = %v", err)
	}
	bus.Start()
	defer bus.Stop()

	body, err := json.Marshal(payentity.NewPaymentCompletedEvent(7, &payentity.Payment{
		ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat,
		BizNo: "ORDER-42", Amount: 100, Currency: "CNY",
	}, &payentity.ChannelNotification{ChannelTradeNo: "wx-trade-1"}))
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	if err := bus.Producer(payentity.TopicPayEvents).Send(context.Background(), body); err != nil {
		t.Fatalf("Send = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if rec, ok := logs.find(slog.LevelInfo, "event received"); ok {
			var payload string
			rec.Attrs(func(a slog.Attr) bool {
				if a.Key == "payload" {
					payload = a.Value.String()
				}
				return true
			})
			if want := `"type":"payment.completed"`; !strings.Contains(payload, want) {
				t.Fatalf("echo payload = %s, want it to contain %s", payload, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("echo subscription never received the event")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
