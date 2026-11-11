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
	"glex/pkg/listq"
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

func (f *fakeRefundRepo) List(context.Context, listq.Query) ([]*payentity.Refund, error) {
	return nil, nil
}

func (f *fakeRefundRepo) Create(ctx context.Context, rf *payentity.Refund) (*payentity.Refund, error) {
	cp := *rf
	f.r[rf.ID] = &cp
	return &cp, nil
}

func (f *fakeRefundRepo) MarkSuccess(ctx context.Context, req payrepo.MarkRefundSuccessRequest) (bool, error) {
	rf, ok := f.r[req.ID]
	if !ok || rf.Status != payentity.RefundPending {
		return false, nil
	}
	rf.Status = payentity.RefundSuccess
	rf.ChannelRefundNo = req.ChannelRefundNo
	refundedAt := req.RefundedAt
	if refundedAt.IsZero() {
		refundedAt = time.Now()
	}
	rf.RefundedAt = &refundedAt
	if f.payments != nil {
		f.payments.addRefunded(req.PaymentID, req.Amount)
	}
	return true, nil
}

func (f *fakeRefundRepo) MarkFailed(ctx context.Context, req payrepo.MarkRefundFailedRequest) (bool, error) {
	rf, ok := f.r[req.ID]
	if !ok || rf.Status != payentity.RefundPending {
		return false, nil
	}
	rf.Status = payentity.RefundFailed
	rf.FailReason = req.Reason
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
	refundState ChannelRefundState
	refundErr   error
	queries     *syncState
	lastRefund  RefundRequest
}

type syncState struct {
	mu      sync.Mutex
	invoked int
}

func (g *scriptRefundGateway) Channel() payentity.Channel { return payentity.ChannelAlipay }

func (g *scriptRefundGateway) Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error) {
	return PrepayResult{}, nil
}

func (g *scriptRefundGateway) Query(ctx context.Context, req QueryRequest) (ChannelPaymentState, error) {
	return ChannelPaymentState{}, nil
}

func (g *scriptRefundGateway) Close(ctx context.Context, req CloseRequest) error {
	return nil
}

func (g *scriptRefundGateway) Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error) {
	g.lastRefund = req
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

func (g *scriptRefundGateway) QueryRefund(ctx context.Context, req RefundQueryRequest) (ChannelRefundState, error) {
	return g.Refund(ctx, RefundRequest{})
}

func (g *scriptRefundGateway) ParseNotification(ctx context.Context, req NotificationRequest) (payentity.ChannelNotification, error) {
	return payentity.ChannelNotification{}, nil
}

func (g *scriptRefundGateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error {
	return nil
}

func (g *scriptRefundGateway) AckSuccess() string { return "" }

// refundFixture is the in-memory refund service plus the rows a test
// reaches through the repository ports. The Pay Application is enabled
// and its refund result address is the literal the copy test expects.
type refundFixture struct {
	svc           Refund
	refunds       *fakeRefundRepo
	payments      *fakePaymentRepo
	apps          *fakePayApplicationRepo
	merchants     *fakeMerchantRepo
	notices       *memNotices
	applicationID int64
	merchantID    int64
	paymentID     int64
}

// newRefundFixture builds the refund service over a PAID payment of 100 fen.
func newRefundFixture(t *testing.T, gw ChannelGateway) refundFixture {
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
	m, err := merchantSvc.Create(context.Background(), wechatInput(7))
	if err != nil {
		t.Fatalf("merchant fixture: %v", err)
	}
	paid.MerchantID = m.ID
	apps := newFakePayApplicationRepo()
	app, err := apps.Create(context.Background(), &payentity.PayApplication{
		ID:              apps.NextID(),
		Name:            "收银台",
		MerchantID:      m.ID,
		Status:          payentity.ApplicationEnabled,
		RefundNotifyURL: "https://shop.example/refunded",
		CreateBy:        7,
		UpdateBy:        7,
	})
	if err != nil {
		t.Fatalf("application fixture: %v", err)
	}
	paid.ApplicationID = app.ID
	if _, err := payments.Create(context.Background(), paid); err != nil {
		t.Fatalf("payment fixture: %v", err)
	}
	notices := &memNotices{}
	svc := NewRefundService(refunds, payments, apps, merchantRepo, &scriptGateways{gw: gw}, notices, memoryTx{payments: payments, refunds: refunds, notices: notices}, slog.New(slog.DiscardHandler))
	return refundFixture{
		svc: svc, refunds: refunds, payments: payments, apps: apps, merchants: merchantRepo, notices: notices,
		applicationID: app.ID, merchantID: m.ID, paymentID: 701,
	}
}

// newTestRefundService builds the refund service over a PAID fixture
// payment of 100 fen on merchant/registry fixtures from the payment
// tests.
func newTestRefundService(t *testing.T, gw ChannelGateway) (Refund, *fakeRefundRepo, *fakePaymentRepo, int64) {
	t.Helper()
	fix := newRefundFixture(t, gw)
	return fix.svc, fix.refunds, fix.payments, fix.paymentID
}

// TestRefundCopiesPayApplicationRefundResultAddress pins the copy at
// creation: the Refund stores the Pay Application's refund result address.
// The literal is the address on the fixture's Pay Application.
func TestRefundCopiesPayApplicationRefundResultAddress(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	svc, _, _, paymentID := newTestRefundService(t, gw)
	rf, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rf.NotifyURL != "https://shop.example/refunded" {
		t.Fatalf("notify url = %q, want https://shop.example/refunded", rf.NotifyURL)
	}
}

// TestRefundCopiesEmptyRefundResultAddress pins the empty address: an
// empty refund result address on the Pay Application is stored empty.
func TestRefundCopiesEmptyRefundResultAddress(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	fix.apps.m[fix.applicationID].RefundNotifyURL = ""
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rf.NotifyURL != "" {
		t.Fatalf("notify url = %q, want empty", rf.NotifyURL)
	}
}

// TestRefundPrepayUsesMerchantCallbackURL pins the channel request: the
// gateway refund call carries the Merchant Account notify address built
// from the deployment base, while the Refund keeps the copied result address.
func TestRefundPrepayUsesMerchantCallbackURL(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := fix.svc.PrepayRefund(context.Background(), PrepayRefundRequest{ID: rf.ID, NotifyBaseURL: "https://pay.example.com"}); err != nil {
		t.Fatalf("prepay: %v", err)
	}
	want := fmt.Sprintf("https://pay.example.com/pay/notify/alipay/%d", fix.merchantID)
	if gw.lastRefund.NotifyURL != want {
		t.Fatalf("channel callback = %q, want %q", gw.lastRefund.NotifyURL, want)
	}
	stored, err := fix.refunds.FindByID(context.Background(), rf.ID)
	if err != nil {
		t.Fatalf("read refund: %v", err)
	}
	if stored.NotifyURL != "https://shop.example/refunded" {
		t.Fatalf("refund result address = %q, want https://shop.example/refunded", stored.NotifyURL)
	}
}

func TestRefundCreateRefusesDisabledApplication(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	fix.apps.m[fix.applicationID].Status = payentity.ApplicationDisabled
	_, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, ActorID: 7})
	if !errors.Is(err, pay.ErrApplicationInvalid) {
		t.Fatalf("err = %v, want ErrApplicationInvalid", err)
	}
	if len(fix.refunds.r) != 0 {
		t.Fatalf("refund rows = %d, want 0", len(fix.refunds.r))
	}
}

func TestRefundCreateRefusesWithdrawnApplication(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	if err := fix.apps.Delete(context.Background(), payrepo.DeletePayApplicationRequest{ID: fix.applicationID, ActorID: 7}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	_, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, ActorID: 7})
	if !errors.Is(err, pay.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
	if len(fix.refunds.r) != 0 {
		t.Fatalf("refund rows = %d, want 0", len(fix.refunds.r))
	}
}

func TestRefundCreateRefusesMissingApplication(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	fix.payments.p[fix.paymentID].ApplicationID = 999999
	_, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, ActorID: 7})
	if !errors.Is(err, pay.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
	if len(fix.refunds.r) != 0 {
		t.Fatalf("refund rows = %d, want 0", len(fix.refunds.r))
	}
}

func TestRefundCreateRefusesDisabledMerchant(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	fix.merchants.m[fix.merchantID].Status = payentity.MerchantDisabled
	_, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, ActorID: 7})
	if !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
	if len(fix.refunds.r) != 0 {
		t.Fatalf("refund rows = %d, want 0", len(fix.refunds.r))
	}
}

func TestRefundCreateRefusesWithdrawnMerchant(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	fix.merchants.deleted[fix.merchantID] = true
	_, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, ActorID: 7})
	if !errors.Is(err, pay.ErrMerchantNotFound) {
		t.Fatalf("err = %v, want ErrMerchantNotFound", err)
	}
	if len(fix.refunds.r) != 0 {
		t.Fatalf("refund rows = %d, want 0", len(fix.refunds.r))
	}
}

func TestRefundMultiplePartialRefundsAccumulate(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundSuccess}}
	svc, refunds, payments, paymentID := newTestRefundService(t, gw)
	// Three partial refunds: 30 + 30 + 40.
	for _, amount := range []int64{30, 30, 40} {
		rf, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: amount, Reason: "部分退款", ActorID: 7})
		if err != nil {
			t.Fatalf("refund %d: %v", amount, err)
		}
		if _, err := svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: rf.ID, State: ChannelRefundState{
			State: payentity.RefundSuccess, ChannelRefundNo: fmt.Sprintf("ali-rf-%d", amount),
		}}); err != nil {
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
	if _, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 101, ActorID: 7}); !errors.Is(err, pay.ErrRefundExceedLimit) {
		t.Fatalf("err = %v, want ErrRefundExceedLimit", err)
	}
	if _, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 0, ActorID: 7}); !errors.Is(err, pay.ErrPaymentInvalid) {
		t.Fatalf("err = %v, want ErrPaymentInvalid", err)
	}
	// A full 100 refund is legal; once it settles, one more fen is over
	// the cap.
	full, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 100, ActorID: 7})
	if err != nil {
		t.Fatalf("full refund rejected: %v", err)
	}
	if _, err := svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: full.ID, State: ChannelRefundState{State: payentity.RefundSuccess}}); err != nil {
		t.Fatalf("settle full refund: %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 1, ActorID: 7}); !errors.Is(err, pay.ErrRefundExceedLimit) {
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
	first, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 60, ActorID: 7})
	if err != nil {
		t.Fatalf("first refund: %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 60, ActorID: 7}); !errors.Is(err, pay.ErrRefundExceedLimit) {
		t.Fatalf("err = %v, want ErrRefundExceedLimit while the first is PENDING", err)
	}
	// The channel fails the first refund: the reservation releases.
	if _, err := svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: first.ID, State: ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"}}); err != nil {
		t.Fatalf("apply failed state: %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 60, ActorID: 7}); err != nil {
		t.Fatalf("retry after release: %v", err)
	}
}

// The concurrent two-refunds-one-cap race is enforced by the payment-
// scoped lock at the application seam — its test lives in
// internal/application/pay (refund_test.go), where the locker is real.

// TestRefundFirstRefundedRecordsOneNotice pins the first transition to
// refunded: one unsigned Business Notification names that Refund and its
// Payment. The address on the notice is the one copied onto the Refund.
func TestRefundFirstRefundedRecordsOneNotice(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := time.Now()
	advanced, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: rf.ID,
		State:    ChannelRefundState{State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-1"},
	})
	after := time.Now()
	if err != nil || !advanced {
		t.Fatalf("apply = (%v, %v), want the first refunded transition", advanced, err)
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindRefund || notice.Status != payentity.NoticeWaiting ||
		notice.RefundID != rf.ID || notice.PaymentID != fix.paymentID || notice.ApplicationID != fix.applicationID ||
		notice.BizNo != "" || notice.NotifyURL != "https://shop.example/refunded" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting refund notice for refund %d payment %d", notice, rf.ID, fix.paymentID)
	}
}

// TestRefundFirstFailedRecordsOneNotice pins the first transition to
// failed: the same unsigned Business Notification, still naming the
// Refund and the Payment.
func TestRefundFirstFailedRecordsOneNotice(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := time.Now()
	advanced, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: rf.ID,
		State:    ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"},
	})
	after := time.Now()
	if err != nil || !advanced {
		t.Fatalf("apply = (%v, %v), want the first failed transition", advanced, err)
	}
	stored, err := fix.refunds.FindByID(context.Background(), rf.ID)
	if err != nil {
		t.Fatalf("read refund: %v", err)
	}
	if stored.Status != payentity.RefundFailed {
		t.Fatalf("refund status = %v, want failed", stored.Status)
	}
	p, err := fix.payments.FindByID(context.Background(), fix.paymentID)
	if err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if p.RefundedAmount != 0 {
		t.Fatalf("refunded_amount = %d, want 0", p.RefundedAmount)
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindRefund || notice.Status != payentity.NoticeWaiting ||
		notice.RefundID != rf.ID || notice.PaymentID != fix.paymentID || notice.ApplicationID != fix.applicationID ||
		notice.BizNo != "" || notice.NotifyURL != "https://shop.example/refunded" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting refund notice for refund %d payment %d", notice, rf.ID, fix.paymentID)
	}
}

// TestRefundEmptyCopiedAddressRecordsNoNotice pins the empty copy: a
// Refund created while the refund result address was empty gets no
// Business Notification. Filling that address on the Pay Application
// afterwards still does not.
func TestRefundEmptyCopiedAddressRecordsNoNotice(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	fix.apps.m[fix.applicationID].RefundNotifyURL = ""
	refunded, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create refunded: %v", err)
	}
	failed, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 10, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if refunded.NotifyURL != "" || failed.NotifyURL != "" {
		t.Fatalf("copied addresses = %q, %q, want empty", refunded.NotifyURL, failed.NotifyURL)
	}
	fix.apps.m[fix.applicationID].RefundNotifyURL = "https://shop.example/later"
	advanced, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: refunded.ID,
		State:    ChannelRefundState{State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-empty"},
	})
	if err != nil || !advanced {
		t.Fatalf("apply refunded = (%v, %v), want the terminal transition", advanced, err)
	}
	advanced, err = fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: failed.ID,
		State:    ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"},
	})
	if err != nil || !advanced {
		t.Fatalf("apply failed = (%v, %v), want the terminal transition", advanced, err)
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %+v, want none when the copied address is empty", fix.notices.rows)
	}
}

// TestRefundSecondTransitionRecordsNoSecondNotice pins the one-notice
// rule: a repeat of the same terminal state does not record again.
func TestRefundSecondTransitionRecordsNoSecondNotice(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	refunded, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create refunded: %v", err)
	}
	failed, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 10, Reason: "部分退款", ActorID: 7})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	success := ChannelRefundState{State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-1"}
	for i := 0; i < 2; i++ {
		advanced, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: refunded.ID, State: success})
		if err != nil {
			t.Fatalf("apply success %d: %v", i, err)
		}
		if i == 0 && !advanced {
			t.Fatal("first success did not advance")
		}
		if i == 1 && advanced {
			t.Fatal("second success advanced again")
		}
	}
	failure := ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"}
	for i := 0; i < 2; i++ {
		advanced, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: failed.ID, State: failure})
		if err != nil {
			t.Fatalf("apply failed %d: %v", i, err)
		}
		if i == 0 && !advanced {
			t.Fatal("first failure did not advance")
		}
		if i == 1 && advanced {
			t.Fatal("second failure advanced again")
		}
	}
	if len(fix.notices.rows) != 2 {
		t.Fatalf("notices = %d, want one per refund", len(fix.notices.rows))
	}
	seen := map[int64]bool{}
	for _, notice := range fix.notices.rows {
		if notice.Kind != payentity.NoticeKindRefund || notice.PaymentID != fix.paymentID {
			t.Fatalf("notice = %+v, want a refund notice for payment %d", notice, fix.paymentID)
		}
		if seen[notice.RefundID] {
			t.Fatalf("refund %d recorded a second notice", notice.RefundID)
		}
		seen[notice.RefundID] = true
	}
	if !seen[refunded.ID] || !seen[failed.ID] {
		t.Fatalf("notices named %v, want refunds %d and %d", seen, refunded.ID, failed.ID)
	}
}

// TestRefundSyncResultRecordsOneNotice pins the synchronous channel
// answer: PrepayRefund settles through the same transition, so a
// refunded or failed answer records one notice, and a still-pending
// answer records none.
func TestRefundSyncResultRecordsOneNotice(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{
		State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-sync", RefundedAt: time.Now(),
	}}
	fix := newRefundFixture(t, gw)
	refunded, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create refunded: %v", err)
	}
	out, err := fix.svc.PrepayRefund(context.Background(), PrepayRefundRequest{ID: refunded.ID, NotifyBaseURL: "https://pay.example.com"})
	if err != nil {
		t.Fatalf("prepay refunded: %v", err)
	}
	if out.Status != payentity.RefundSuccess {
		t.Fatalf("refund status = %v, want refunded", out.Status)
	}
	gw.refundState = ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"}
	failed, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 10, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	out, err = fix.svc.PrepayRefund(context.Background(), PrepayRefundRequest{ID: failed.ID, NotifyBaseURL: "https://pay.example.com"})
	if err != nil {
		t.Fatalf("prepay failed: %v", err)
	}
	if out.Status != payentity.RefundFailed {
		t.Fatalf("refund status = %v, want failed", out.Status)
	}
	gw.refundState = ChannelRefundState{State: payentity.RefundPending}
	pending, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 10, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	out, err = fix.svc.PrepayRefund(context.Background(), PrepayRefundRequest{ID: pending.ID, NotifyBaseURL: "https://pay.example.com"})
	if err != nil {
		t.Fatalf("prepay pending: %v", err)
	}
	if out.Status != payentity.RefundPending {
		t.Fatalf("refund status = %v, want pending", out.Status)
	}
	if len(fix.notices.rows) != 2 {
		t.Fatalf("notices = %d, want one for refunded and one for failed", len(fix.notices.rows))
	}
	seen := map[int64]bool{}
	for _, notice := range fix.notices.rows {
		if notice.Kind != payentity.NoticeKindRefund || notice.PaymentID != fix.paymentID || notice.BizNo != "" ||
			notice.NotifyURL != "https://shop.example/refunded" || notice.Status != payentity.NoticeWaiting {
			t.Fatalf("notice = %+v, want a waiting refund notice for payment %d", notice, fix.paymentID)
		}
		seen[notice.RefundID] = true
	}
	if !seen[refunded.ID] || !seen[failed.ID] || seen[pending.ID] {
		t.Fatalf("notices named %v, want refunds %d and %d only", seen, refunded.ID, failed.ID)
	}
}

// TestRefundQueryRecordsOneNotice pins the lost-callback settlement
// path: Query applies the channel's answer through the same transition.
func TestRefundQueryRecordsOneNotice(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{
		State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-query",
	}}
	fix := newRefundFixture(t, gw)
	refunded, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create refunded: %v", err)
	}
	out, err := fix.svc.Query(context.Background(), refunded.ID)
	if err != nil {
		t.Fatalf("query refunded: %v", err)
	}
	if out.Status != payentity.RefundSuccess {
		t.Fatalf("refund status = %v, want refunded", out.Status)
	}
	gw.refundState = ChannelRefundState{State: payentity.RefundFailed, FailReason: "insufficient balance"}
	failed, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 10, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	out, err = fix.svc.Query(context.Background(), failed.ID)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if out.Status != payentity.RefundFailed {
		t.Fatalf("refund status = %v, want failed", out.Status)
	}
	if len(fix.notices.rows) != 2 {
		t.Fatalf("notices = %d, want one per queried refund", len(fix.notices.rows))
	}
	seen := map[int64]bool{}
	for _, notice := range fix.notices.rows {
		if notice.Kind != payentity.NoticeKindRefund || notice.PaymentID != fix.paymentID || notice.RefundID == 0 {
			t.Fatalf("notice = %+v, want a refund notice for payment %d", notice, fix.paymentID)
		}
		seen[notice.RefundID] = true
	}
	if !seen[refunded.ID] || !seen[failed.ID] {
		t.Fatalf("notices named %v, want refunds %d and %d", seen, refunded.ID, failed.ID)
	}
}

// TestRefundNoticeInsertFailureLeavesTheRefundPending pins a failed insert:
// the refund stays pending and the payment's refunded sum does not move.
func TestRefundNoticeInsertFailureLeavesTheRefundPending(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fix.notices.err = errors.New("store down")
	if _, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: rf.ID,
		State:    ChannelRefundState{State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-1"},
	}); err == nil {
		t.Fatal("apply succeeded, want the refunded transition withheld when the notice cannot commit")
	}
	stored, err := fix.refunds.FindByID(context.Background(), rf.ID)
	if err != nil {
		t.Fatalf("read refund: %v", err)
	}
	if stored.Status != payentity.RefundPending || stored.ChannelRefundNo != "" {
		t.Fatalf("refund = %+v, want still pending", stored)
	}
	p, err := fix.payments.FindByID(context.Background(), fix.paymentID)
	if err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if p.RefundedAmount != 0 {
		t.Fatalf("refunded_amount = %d, want 0", p.RefundedAmount)
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %d, want no due row without the refunded payment", len(fix.notices.rows))
	}
}

func TestFailedRefundNoticeInsertFailureLeavesTheRefundPending(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fix.notices.err = errors.New("store down")
	if _, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: rf.ID,
		State:    ChannelRefundState{State: payentity.RefundFailed, FailReason: "channel rejected"},
	}); err == nil {
		t.Fatal("apply succeeded, want the failed transition withheld when the notice cannot commit")
	}
	stored, err := fix.refunds.FindByID(context.Background(), rf.ID)
	if err != nil {
		t.Fatalf("read refund: %v", err)
	}
	if stored.Status != payentity.RefundPending || stored.FailReason != "" {
		t.Fatalf("refund = %+v, want still pending", stored)
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %d, want no due row without the failed refund", len(fix.notices.rows))
	}
}

// TestRefundDuplicateNoticeInsertKeepsTheTransition pins the unique pair:
// ok false from the notice insert is not an error, and the Refund stays
// refunded.
func TestRefundDuplicateNoticeInsertKeepsTheTransition(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{State: payentity.RefundPending}}
	fix := newRefundFixture(t, gw)
	rf, err := fix.svc.Create(context.Background(), CreateRefundRequest{PaymentID: fix.paymentID, Amount: 40, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fix.notices.refuse = true
	advanced, err := fix.svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{
		RefundNo: rf.ID,
		State:    ChannelRefundState{State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-1"},
	})
	if err != nil || !advanced {
		t.Fatalf("apply = (%v, %v), want the refunded transition kept", advanced, err)
	}
	stored, err := fix.refunds.FindByID(context.Background(), rf.ID)
	if err != nil {
		t.Fatalf("read refund: %v", err)
	}
	if stored.Status != payentity.RefundSuccess {
		t.Fatalf("refund status = %v, want refunded", stored.Status)
	}
}

func TestRefundSyncChannelSettlesImmediately(t *testing.T) {
	gw := &scriptRefundGateway{refundState: ChannelRefundState{
		State: payentity.RefundSuccess, ChannelRefundNo: "ali-rf-1", RefundedAt: time.Now(),
	}}
	svc, refunds, payments, paymentID := newTestRefundService(t, gw)
	rf, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 40, Reason: "x", ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := svc.PrepayRefund(context.Background(), PrepayRefundRequest{ID: rf.ID, NotifyBaseURL: "https://pay.example.com"})
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
	rf, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 40, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The channel callback and a query both report success.
	for i := 0; i < 2; i++ {
		advanced, err := svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: rf.ID, State: ChannelRefundState{State: payentity.RefundSuccess}})
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
	rf, err := svc.Create(context.Background(), CreateRefundRequest{PaymentID: paymentID, Amount: 40, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A signature-valid success naming the wrong refund amount never settles.
	advanced, err := svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: rf.ID, State: ChannelRefundState{
		State: payentity.RefundSuccess, RefundAmount: 999,
	}})
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
	advanced, err = svc.ApplyRefundState(context.Background(), ApplyRefundStateRequest{RefundNo: rf.ID, State: ChannelRefundState{
		State: payentity.RefundSuccess, RefundAmount: 40,
	}})
	if err != nil || !advanced {
		t.Fatalf("verified advanced = %v, err = %v", advanced, err)
	}
	p, _ = payments.FindByID(context.Background(), paymentID)
	if p.RefundedAmount != 40 {
		t.Fatalf("refunded_amount = %d, want 40", p.RefundedAmount)
	}
}
