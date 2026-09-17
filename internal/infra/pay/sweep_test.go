package payinfra

import (
	"context"
	"errors"
	"testing"

	pay "glex/internal/domain/pay"
	payservice "glex/internal/domain/pay/service"
)

// fakeSweepService answers only the sweep call; the embedded nil interface
// panics on anything else, which no sweep test reaches.
type fakeSweepService struct {
	payservice.Payment
	swept  int
	limit  int
	sweepFails bool
}

func (f *fakeSweepService) SweepExpired(_ context.Context, limit int) (int, error) {
	f.limit = limit
	if f.sweepFails {
		return 0, pay.ErrChannelError
	}
	return f.swept, nil
}

func TestExpireSweepExecutorName(t *testing.T) {
	e := NewExpireSweepExecutor(&fakeSweepService{})
	if e.Name() != "pay.expire-sweep" {
		t.Fatalf("name = %q", e.Name())
	}
}

func TestExpireSweepExecutorRunSummarizes(t *testing.T) {
	fake := &fakeSweepService{swept: 3}
	e := NewExpireSweepExecutor(fake)
	summary, err := e.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary != "settled 3 expired pending payment(s)" {
		t.Fatalf("summary = %q", summary)
	}
	if fake.limit != expireSweepBatch {
		t.Fatalf("batch limit = %d, want %d", fake.limit, expireSweepBatch)
	}
}

func TestExpireSweepExecutorRunPropagatesError(t *testing.T) {
	e := NewExpireSweepExecutor(&fakeSweepService{sweepFails: true})
	if _, err := e.Run(context.Background(), ""); !errors.Is(err, pay.ErrChannelError) {
		t.Fatalf("err = %v, want ErrChannelError", err)
	}
}
