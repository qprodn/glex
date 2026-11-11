package pay

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	payv1 "glex/api/pay/v1"
	"glex/internal/conf"
	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/lock"
)

// The refund-cap race test (ticket 07): two concurrent refunds of one
// payment competing for the same remaining cap. The payment-scoped lock
// serializes them, so exactly one refund wins and the loser sees
// LOCK_BUSY (HTTP 409) — the same duplicate-submit semantics as payments.

// serialLocker is a real fail-fast mutex in Locker's shape.
type serialLocker struct {
	mu   sync.Mutex
	held bool
	keys []string
}

func (l *serialLocker) WithLock(ctx context.Context, key string, fn func(ctx context.Context) error, opts ...lock.Option) error {
	l.mu.Lock()
	l.keys = append(l.keys, key)
	if l.held {
		l.mu.Unlock()
		return lock.ErrLockBusy
	}
	l.held = true
	l.mu.Unlock()
	err := fn(ctx)
	l.mu.Lock()
	l.held = false
	l.mu.Unlock()
	return err
}

// racingRefundSvc simulates the refund domain service's cap check with a
// slice of state shared by the racing callers: the cap check reads the
// counter, records the new refund, and refuses when the sum would break.
type racingRefundSvc struct {
	payservice.Refund
	mu        sync.Mutex
	paid      int64
	refunded  int64
	dropAfter int // how many callers see ErrRefundExceedLimit once capped
}

func (f *racingRefundSvc) Create(ctx context.Context, req payservice.CreateRefundRequest) (*payentity.Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refunded+req.Amount > f.paid {
		return nil, pay.ErrRefundExceedLimit
	}
	f.refunded += req.Amount
	return &payentity.Refund{ID: 1, PaymentID: req.PaymentID, Amount: req.Amount, Status: payentity.RefundPending}, nil
}

func (f *racingRefundSvc) PrepayRefund(ctx context.Context, req payservice.PrepayRefundRequest) (*payentity.Refund, error) {
	return &payentity.Refund{ID: req.ID, Status: payentity.RefundSuccess}, nil
}

func TestCreateRefundConcurrentCapRaceExactlyOneWins(t *testing.T) {
	refunds := &racingRefundSvc{paid: 100}
	locker := &serialLocker{}
	app := NewPayApplication(
		&fakeMerchantSvc{},
		nil,
		&fakePaymentSvc{},
		refunds,
		nil,
		nil,
		nil,
		&fakeTx{},
		locker,
		&conf.Pay{},
		slog.New(slog.DiscardHandler),
	)
	// Two 60-fen refunds against a 100-fen payment: together impossible.
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := app.CreateRefund(context.Background(), &payv1.CreateRefundRequest{
				PaymentId: 701,
				Amount:    60,
			})
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)

	var wins, busies, capped int
	for err := range outcomes {
		switch reason := func() string {
			if e := kratoserrors.FromError(err); e != nil {
				return e.Reason
			}
			return ""
		}(); {
		case err == nil:
			wins++
		case reason == "LOCK_BUSY":
			busies++ // refused at the lock: the winner was mid-flight
		case reason == "REFUND_EXCEED_LIMIT":
			capped++ // serialized after the winner: refused by the cap
		default:
			t.Fatalf("unexpected outcome: %v", err)
		}
	}
	if wins != 1 || wins+busies+capped != 2 {
		t.Fatalf("wins = %d, busies = %d, capped = %d; want exactly one winner", wins, busies, capped)
	}
	// Both submissions contended on the payment-scoped lock.
	if len(locker.keys) != 2 || locker.keys[0] != "pay:refund:701" || locker.keys[1] != "pay:refund:701" {
		t.Fatalf("lock keys = %v", locker.keys)
	}
	if refunds.refunded != 60 {
		t.Fatalf("refunded = %d, want 60 — the loser never moved money", refunds.refunded)
	}
}
