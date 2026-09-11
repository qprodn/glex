package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/pay/crypt"
)

// fakeRefundRepo mirrors the CAS refund state machine in memory.
type fakeRefundRepo struct {
	r      map[int64]*payentity.Refund
	nextID int64
	// payments mirrors the atomic refunded-sum advance the real
	// MarkSuccess performs inside its transaction; wired by the fixture.
	payments *fakePaymentRepo
}

func newFakeRefundRepo() *fakeRefundRepo {
	return &fakeRefundRepo{r: map[int64]*payentity.Refund{}, nextID: 600}
}

func (f *fakeRefundRepo) FindByID(ctx context.Context, id int64) (*payentity.Refund, error) {
	if rf, ok := f.r[id]; ok {
		cp := *rf
		return &cp, nil
	}
	return nil, pay.ErrRefundNotFound
}

func (f *fakeRefundRepo) List(ctx context.Context, opts ...payrepo.ListOption) ([]*payentity.Refund, error) {
	return nil, nil
}

func (f *fakeRefundRepo) Create(ctx context.Context, rf *payentity.Refund) (*payentity.Refund, error) {
	cp := *rf
	f.r[rf.ID] = &cp
	return &cp, nil
}

func (f *fakeRefundRepo) MarkSuccess(ctx context.Context, id, paymentID, amount int64, channelRefundNo string, refundedAt time.Time) (bool, error) {
	rf, ok := f.r[id]
	if !ok || rf.Status != payentity.RefundPending {
		return false, nil
	}
	rf.Status = payentity.RefundSuccess
	rf.ChannelRefundNo = channelRefundNo
	if refundedAt.IsZero() {
		refundedAt = time.Now()
	}
	rf.RefundedAt = &refundedAt
	if f.payments != nil {
		f.payments.addRefunded(paymentID, amount)
	}
	return true, nil
}

func (f *fakeRefundRepo) MarkFailed(ctx context.Context, id int64, reason string) (bool, error) {
	rf, ok := f.r[id]
	if !ok || rf.Status != payentity.RefundPending {
		return false, nil
	}
	rf.Status = payentity.RefundFailed
	rf.FailReason = reason
	return true, nil
}

// PendingRefundSum mirrors the SQL aggregate over in-flight refunds.
func (f *fakeRefundRepo) PendingRefundSum(ctx context.Context, paymentID int64) (int64, error) {
	var sum int64
	for _, rf := range f.r {
		if rf.PaymentID == paymentID && rf.Status == payentity.RefundPending {
			sum += rf.Amount
		}
	}
	return sum, nil
}

func (f *fakeRefundRepo) NextID() int64 { f.nextID++; return f.nextID }

// scriptRefundGateway answers refunds synchronously and reports refund
// queries on demand.
type scriptRefundGateway struct {
	refundState  ChannelRefundState
	refundErr    error
	queries      *syncState
}

type syncState struct {
	mu       sync.Mutex
	invoked  int
}

func (g *scriptRefundGateway) Channel() payentity.Channel { return payentity.ChannelAlipay }

func (g *scriptRefundGateway) Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error) {
	return PrepayResult{}, nil
}

func (g *scriptRefundGateway) Query(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) (ChannelPaymentState, error) {
	return ChannelPaymentState{}, nil
}

func (g *scriptRefundGateway) Close(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) error {
	return nil
}

func (g *scriptRefundGateway) Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error) {
	if g.queries != nil {
		g.queries.mu.Lock()
		g.queries.invoked++
		g.queries.mu.Unlock()
	}
	if g.refundErr != nil {
		return ChannelRefundState{}, g.refundErr
	}
	return g.refundState, nil
}

func (g *scriptRefundGateway) QueryRefund(ctx context.Context, m *payentity.MerchantAccount, refundNo, paymentNo string) (ChannelRefundState, error) {
	return g.Refund(ctx, RefundRequest{})
}

func (g *scriptRefundGateway) ParseNotification(ctx context.Context, m *payentity.MerchantAccount, headers httpHeader, body []byte) (payentity.ChannelNotification, error) {
	return payentity.ChannelNotification{}, nil
}

func (g *scriptRefundGateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error {
	return nil
}

// newTestRefundService builds the refund service over a PAID fixture
// payment of 100 fen on merchant/registry fixtures from the payment
// tests.
func newTestRefundService(t *testing.T, gw ChannelGateway) (Refund, *fakeRefundRepo, *fakePaymentRepo, int64) {
	t.Helper()
	payments := newFakePaymentRepo()
	payments.nextID = 700
	_ = payments
	paid := &payentity.Payment{
		ID:         701,
		Channel:    payentity.ChannelAlipay,
		BizNo:      "ORDER-R",
		Subject:    "退款测试",
		Amount:     100,
		Currency:   "CNY",
		PayMethod:  payentity.PayMethodNative,
		Status:     payentity.PaymentPaid,
		ExpireTime: time.Now().Add(time.Hour),
	}
	refunds := newFakeRefundRepo()
	refunds.payments = payments
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	cipher := crypt.New()
	merchantRepo := newFakeMerchantRepo(cipher)
	// The merchant fixture backs the payment's merchant_id snapshot.
	catalog := &fakeCatalog{schemas: []payentity.ChannelSchema{wechatSchema()}}
	merchantSvc := NewMerchantService(merchantRepo, catalog, cipher, &scriptGateways{}, slog.New(slog.DiscardHandler))
	m, err := merchantSvc.Create(context.Background(), wechatInput(), 7)
	if err != nil {
		t.Fatalf("merchant fixture: %v", err)
	}
	paid.MerchantID = m.ID
	if _, err := payments.Create(context.Background(), paid); err != nil {
		t.Fatalf("payment fixture: %v", err)
	}
	svc := NewRefundService(refunds, payments, merchantRepo, &scriptGateways{gw: gw}, slog.New(slog.DiscardHandler))
	return svc, refunds, payments, 701
}

func TestRefundMultiplePartialRefundsAccumulate(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundSuccess}}
	svc, refunds, payments, paymentID := newTestRefundService(t, gw)
	// Three partial refunds: 30 + 30 + 40.
	for _, amount := range []int64{30, 30, 40} {
		rf, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: amount, Reason: "部分退款"}, 7)
		if err != nil {
			t.Fatalf("refund %d: %v", amount, err)
		}
		if _, err := svc.ApplyRefundState(context.Background(), rf.ID, ChannelRefundState{
			State: payentity.RefundSuccess, ChannelRefundNo: fmt.Sprintf("ali-rf-%d", amount),
		}); err != nil {
			t.Fatalf("apply %d: %v", amount, err)
		}
	}
	p, _ := payments.FindByID(context.Background(), paymentID)
	if p.Status != payentity.PaymentPaid {
		t.Fatalf("payment status = %v — refunds never change it", p.Status)
	}
	if p.RefundedAmount != 100 {
		t.Fatalf("refunded_amount = %d, want 100", p.RefundedAmount)
	}
	if len(refunds.r) != 3 {
		t.Fatalf("refund rows = %d, want 3", len(refunds.r))
	}
}

// TestRefundExceedsCapRejected pins the cap: refunded + this ≤ paid, with
// the rejection a Warn-grade business error (ErrRefundExceedLimit), never
// a system failure.
func TestRefundExceedsCapRejected(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundSuccess}}
	svc, _, _, paymentID := newTestRefundService(t, gw)
	if _, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 101}, 7); !errors.Is(err, pay.ErrRefundExceedLimit) {
		t.Fatalf("err = %v, want ErrRefundExceedLimit", err)
	}
	if _, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 0}, 7); !errors.Is(err, pay.ErrPaymentInvalid) {
		t.Fatalf("err = %v, want ErrPaymentInvalid", err)
	}
	// A full 100 refund is legal; once it settles, one more fen is over
	// the cap.
	full, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 100}, 7)
	if err != nil {
		t.Fatalf("full refund rejected: %v", err)
	}
	if _, err := svc.ApplyRefundState(context.Background(), full.ID, ChannelRefundState{State: payentity.RefundSuccess}); err != nil {
		t.Fatalf("settle full refund: %v", err)
	}
	if _, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 1}, 7); !errors.Is(err, pay.ErrRefundExceedLimit) {
		t.Fatalf("err = %v, want ErrRefundExceedLimit", err)
	}
}

// TestRefundPendingReservesCap pins the async-refund reservation: a
// PENDING refund of 60 on a 100 payment blocks a second 60 until it
// settles or fails — the create-time check counts in-flight amounts
// (ticket 07).
func TestRefundPendingReservesCap(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	svc, _, _, paymentID := newTestRefundService(t, gw)
	first, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 60}, 7)
	if err != nil {
		t.Fatalf("first refund: %v", err)
	}
	if _, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 60}, 7); !errors.Is(err, pay.ErrRefundExceedLimit) {
		t.Fatalf("err = %v, want ErrRefundExceedLimit while the first is PENDING", err)
	}
	// The channel fails the first refund: the reservation releases.
	if _, err := svc.ApplyRefundState(context.Background(), first.ID, ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"}); err != nil {
		t.Fatalf("apply failed state: %v", err)
	}
	if _, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 60}, 7); err != nil {
		t.Fatalf("retry after release: %v", err)
	}
}

// The concurrent two-refunds-one-cap race is enforced by the payment-
// scoped lock at the application seam — its test lives in
// internal/application/pay (refund_test.go), where the locker is real.

func TestRefundSyncChannelSettlesImmediately(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{
		State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-1", RefundedAt: time.Now(),
	}}
	svc, refunds, payments, paymentID := newTestRefundService(t, gw)
	rf, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 40, Reason: "x"}, 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := svc.PrepayRefund(context.Background(), rf.ID, "https://pay.example.com")
	if err != nil {
		t.Fatalf("prepay refund: %v", err)
	}
	if out.Status != payentity.RefundSuccess || out.ChannelRefundNo != "ali-rf-1" {
		t.Fatalf("refund = %+v", out)
	}
	// The payment's sum moved with the settlement.
	p, _ := payments.FindByID(context.Background(), paymentID)
	if p.RefundedAmount != 40 {
		t.Fatalf("refunded_amount = %d, want 40", p.RefundedAmount)
	}
	_ = refunds
}

func TestRefundApplyStateDuplicateDoesNotDoubleCount(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundSuccess}}
	svc, refunds, payments, paymentID := newTestRefundService(t, gw)
	rf, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 40}, 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The channel callback and a query both report success.
	for i := 0; i < 2; i++ {
		advanced, err := svc.ApplyRefundState(context.Background(), rf.ID, ChannelRefundState{State: payentity.RefundSuccess})
		if err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
		if i == 0 && !advanced {
			t.Fatal("first success did not advance")
		}
		if i == 1 && advanced {
			t.Fatal("duplicate success advanced again")
		}
	}
	p, _ := payments.FindByID(context.Background(), paymentID)
	if p.RefundedAmount != 40 {
		t.Fatalf("refunded_amount = %d, want 40 (no double count)", p.RefundedAmount)
	}
	stored := refunds.r[rf.ID]
	if stored.Status != payentity.RefundSuccess {
		t.Fatalf("refund status = %v", stored.Status)
	}
}

func TestRefundApplyStateRejectsRefundAmountMismatch(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundSuccess}}
	svc, refunds, payments, paymentID := newTestRefundService(t, gw)
	rf, err := svc.Create(context.Background(), &CreateRefundInput{PaymentID: paymentID, Amount: 40}, 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A signature-valid success naming the wrong refund amount never settles.
	advanced, err := svc.ApplyRefundState(context.Background(), rf.ID, ChannelRefundState{
		State: payentity.RefundSuccess, RefundAmount: 999,
	})
	if err != nil || advanced {
		t.Fatalf("mismatch advanced = %v, err = %v", advanced, err)
	}
	stored := refunds.r[rf.ID]
	if stored.Status != payentity.RefundPending {
		t.Fatalf("status = %v — a mismatched amount must not settle", stored.Status)
	}
	p, _ := payments.FindByID(context.Background(), paymentID)
	if p.RefundedAmount != 0 {
		t.Fatalf("refunded_amount = %d, want 0", p.RefundedAmount)
	}
	// The verified amount settles and moves the sum.
	advanced, err = svc.ApplyRefundState(context.Background(), rf.ID, ChannelRefundState{
		State: payentity.RefundSuccess, RefundAmount: 40,
	})
	if err != nil || !advanced {
		t.Fatalf("verified advanced = %v, err = %v", advanced, err)
	}
	p, _ = payments.FindByID(context.Background(), paymentID)
	if p.RefundedAmount != 40 {
		t.Fatalf("refunded_amount = %d, want 40", p.RefundedAmount)
	}
}
