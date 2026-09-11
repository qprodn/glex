package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/pay/crypt"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// fakePaymentRepo implements the CAS state machine honestly in memory: a
// transition wins only when the row is still Pending (the WHERE status=1
// of the SQL statement), so out-of-order receipts behave exactly like they
// do against PostgreSQL.
type fakePaymentRepo struct {
	p        map[int64]*payentity.Payment
	nextID   int64
	resolved int // SetChannelPayload + FindByID volume, for assertions
}

func newFakePaymentRepo() *fakePaymentRepo {
	return &fakePaymentRepo{p: map[int64]*payentity.Payment{}, nextID: 500}
}

func (f *fakePaymentRepo) FindByID(ctx context.Context, id int64) (*payentity.Payment, error) {
	if p, ok := f.p[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, pay.ErrPaymentNotFound
}

func (f *fakePaymentRepo) List(ctx context.Context, opts ...payrepo.ListOption) ([]*payentity.Payment, error) {
	return nil, nil
}

func (f *fakePaymentRepo) Create(ctx context.Context, p *payentity.Payment) (*payentity.Payment, error) {
	cp := *p
	f.p[p.ID] = &cp
	return &cp, nil
}

func (f *fakePaymentRepo) ClosePendingByBizNo(ctx context.Context, merchantID int64, bizNo string, actorID int64) (int, error) {
	n := 0
	for _, p := range f.p {
		if p.MerchantID == merchantID && p.BizNo == bizNo && p.Status == payentity.PaymentPending {
			p.Status = payentity.PaymentClosed
			p.UpdateBy = actorID
			n++
		}
	}
	return n, nil
}

func (f *fakePaymentRepo) SetChannelPayload(ctx context.Context, id int64, payload string) error {
	f.p[id].ChannelPayload = payload
	f.resolved++
	return nil
}

func (f *fakePaymentRepo) SetFailReason(ctx context.Context, id int64, reason string) error {
	f.p[id].FailReason = reason
	return nil
}

func (f *fakePaymentRepo) MarkPaid(ctx context.Context, id int64, capture payentity.PaidCapture) (bool, error) {
	p, ok := f.p[id]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentPaid
	p.ChannelTradeNo = capture.ChannelTradeNo
	p.PayerID = capture.PayerID
	if !capture.PaidAt.IsZero() {
		paidAt := capture.PaidAt
		p.PaidAt = &paidAt
	}
	return true, nil
}

func (f *fakePaymentRepo) BackfillPaidInfo(ctx context.Context, id int64, capture payentity.PaidCapture) (bool, error) {
	p, ok := f.p[id]
	if !ok || p.Status != payentity.PaymentPaid {
		return false, nil
	}
	changed := false
	if capture.ChannelTradeNo != "" && p.ChannelTradeNo == "" {
		p.ChannelTradeNo = capture.ChannelTradeNo
		changed = true
	}
	if capture.PayerID != "" && p.PayerID == "" {
		p.PayerID = capture.PayerID
		changed = true
	}
	return changed, nil
}

func (f *fakePaymentRepo) MarkFailed(ctx context.Context, id int64, reason string) (bool, error) {
	p, ok := f.p[id]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentFailed
	p.FailReason = reason
	return true, nil
}

func (f *fakePaymentRepo) MarkClosed(ctx context.Context, id int64) (bool, error) {
	p, ok := f.p[id]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentClosed
	return true, nil
}

func (f *fakePaymentRepo) MarkExpired(ctx context.Context, id int64) (bool, error) {
	p, ok := f.p[id]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentExpired
	return true, nil
}

func (f *fakePaymentRepo) AccumulateRefunded(ctx context.Context, id int64, amount int64) (bool, error) {
	p, ok := f.p[id]
	if !ok || p.Status != payentity.PaymentPaid {
		return false, nil
	}
	p.RefundedAmount += amount
	return true, nil
}

func (f *fakePaymentRepo) NextID() int64 { f.nextID++; return f.nextID }

// scriptGateway is the contract-test gateway: every behavior the payment
// tests need, nothing more.
type scriptGateway struct {
	prepayErr   error
	prepayBody  string
	queryState  ChannelPaymentState
	queryErr    error
	closeErr    error
	closeCalled *int
	resolveHits *int
}

func (g *scriptGateway) Channel() payentity.Channel { return payentity.ChannelWechat }

func (g *scriptGateway) Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error) {
	if g.resolveHits != nil {
		*g.resolveHits++
	}
	if g.prepayErr != nil {
		return PrepayResult{}, g.prepayErr
	}
	return PrepayResult{ChannelPayload: g.prepayBody}, nil
}

func (g *scriptGateway) Query(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) (ChannelPaymentState, error) {
	if g.resolveHits != nil {
		*g.resolveHits++
	}
	if g.queryErr != nil {
		return ChannelPaymentState{}, g.queryErr
	}
	return g.queryState, nil
}

func (g *scriptGateway) Close(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) error {
	if g.closeCalled != nil {
		*g.closeCalled++
	}
	return g.closeErr
}

func (g *scriptGateway) Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}

func (g *scriptGateway) QueryRefund(ctx context.Context, m *payentity.MerchantAccount, refundNo string) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}

func (g *scriptGateway) ParseNotification(ctx context.Context, m *payentity.MerchantAccount, headers httpHeader, body []byte) (payentity.ChannelNotification, error) {
	return payentity.ChannelNotification{}, nil
}

// httpHeader aliases net/http's Header type under a short test-local name.
type httpHeader = http.Header

func (g *scriptGateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error { return nil }

// scriptGateways feeds the registry port a single scriptable gateway.
type scriptGateways struct {
	gw ChannelGateway
}

func (s *scriptGateways) Resolve(ctx context.Context, m *payentity.MerchantAccount) (ChannelGateway, error) {
	return s.gw, nil
}

func (s *scriptGateways) Invalidate(merchantID int64) {}

// newTestPaymentService builds the payment service over fakes, together
// with the payment repo (for direct row manipulation) and the fixture
// merchant's id.
func newTestPaymentService(t *testing.T, gw *scriptGateway) (Payment, *fakePaymentRepo, int64) {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	cipher := crypt.New()
	repo := newFakePaymentRepo()
	repo.nextID = 500
	merchantRepo := newFakeMerchantRepo(cipher)
	catalog := &fakeCatalog{schemas: []payentity.ChannelSchema{wechatSchema()}}
	merchantSvc := NewMerchantService(merchantRepo, catalog, cipher, &scriptGateways{}, slog.New(slog.DiscardHandler))
	m, err := merchantSvc.Create(context.Background(), wechatInput(), 7)
	if err != nil {
		t.Fatalf("create merchant fixture: %v", err)
	}
	svc := NewPaymentService(repo, merchantRepo, &scriptGateways{gw: gw}, slog.New(slog.DiscardHandler))
	return svc, repo, m.ID
}

func ctx() context.Context { return context.Background() }

func orderByDefault() ordering.OrderBy {
	var o ordering.OrderBy
	o.Fields = []ordering.Field{{Path: "id", Desc: true}}
	return o
}

func initiateInput(merchantID int64) *CreatePaymentInput {
	return &CreatePaymentInput{
		MerchantID: merchantID,
		BizNo:      "ORDER-1",
		Subject:    "测试商品",
		Amount:     100,
		PayMethod:  payentity.PayMethodNative,
	}
}

func TestInitiateClosesOldPendingAndCreatesNew(t *testing.T) {
	svc, repo, merchantID := newTestPaymentService(t, &scriptGateway{prepayBody: "wxp://qr"})
	// A first pending payment exists for (merchant, ORDER-1).
	first, err := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	if err != nil {
		t.Fatalf("first initiate: %v", err)
	}
	if first.Status != payentity.PaymentPending || first.Channel != payentity.ChannelWechat {
		t.Fatalf("first payment = %+v", first)
	}
	// Re-initiation closes the old pending row and inserts a fresh one.
	second, err := svc.Initiate(context.Background(), initiateInput(merchantID), 8)
	if err != nil {
		t.Fatalf("second initiate: %v", err)
	}
	firstAfter, _ := repo.FindByID(context.Background(), first.ID)
	if firstAfter.Status != payentity.PaymentClosed {
		t.Fatalf("old pending not closed: %v", firstAfter.Status)
	}
	if second.ID == first.ID || second.Status != payentity.PaymentPending || second.CreateBy != 8 {
		t.Fatalf("second payment = %+v", second)
	}
	if second.ExpireTime.Before(time.Now()) {
		t.Fatal("new payment already expired")
	}
}

func TestInitiateValidations(t *testing.T) {
	svc, _, merchantID := newTestPaymentService(t, &scriptGateway{})
	in := initiateInput(merchantID)
	in.Amount = 0
	if _, err := svc.Initiate(context.Background(), in, 7); !errors.Is(err, pay.ErrPaymentInvalid) {
		t.Fatalf("err = %v, want ErrPaymentInvalid", err)
	}
	in = initiateInput(merchantID)
	in.MerchantID = 999999
	if _, err := svc.Initiate(context.Background(), in, 7); !errors.Is(err, pay.ErrMerchantNotFound) {
		t.Fatalf("err = %v, want ErrMerchantNotFound", err)
	}
	in = initiateInput(merchantID)
	in.BizNo = ""
	if _, err := svc.Initiate(context.Background(), in, 7); !errors.Is(err, pay.ErrPaymentInvalid) {
		t.Fatalf("err = %v, want ErrPaymentInvalid", err)
	}
}

func TestPrepayBackfillsPayload(t *testing.T) {
	svc, _, merchantID := newTestPaymentService(t, &scriptGateway{prepayBody: "wxp://faked"})
	p, err := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	out, err := svc.Prepay(context.Background(), p.ID, "https://pay.example.com")
	if err != nil {
		t.Fatalf("prepay: %v", err)
	}
	if out.ChannelPayload != "wxp://faked" {
		t.Fatalf("payload = %q", out.ChannelPayload)
	}
}

func TestPrepayChannelFailureFailsThePayment(t *testing.T) {
	svc, repo, merchantID := newTestPaymentService(t, &scriptGateway{prepayErr: errors.New("ORDER_CLOSED by channel")})
	p, err := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	_, err = svc.Prepay(context.Background(), p.ID, "")
	if !errors.Is(err, pay.ErrChannelError) {
		t.Fatalf("err = %v, want ErrChannelError", err)
	}
	stored, _ := repo.FindByID(context.Background(), p.ID)
	if stored.Status != payentity.PaymentFailed || stored.FailReason == "" {
		t.Fatalf("failed payment = %+v — no payable illusion may survive", stored)
	}
}

func TestGetSettlesExpiredPaymentToPaid(t *testing.T) {
	var closeCalls int
	gw := &scriptGateway{
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-77",
			PayerID:        "openid-77",
			PaidAt:         time.Now(),
		},
		closeCalled: &closeCalls,
	}
	svc, repo, merchantID := newTestPaymentService(t, gw)
	p, err := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	// Force the row past its expiry.
	repo.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
	out, err := svc.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-77" || out.PaidAt == nil {
		t.Fatalf("settled payment = %+v", out)
	}
}

func TestGetSettlesExpiredPaymentToExpired(t *testing.T) {
	var closeCalls int
	gw := &scriptGateway{
		queryState:  ChannelPaymentState{State: payentity.PaymentPending},
		closeCalled: &closeCalls,
	}
	svc, repo, merchantID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	repo.p[p.ID].ExpireTime = time.Now().Add(-time.Minute)
	out, err := svc.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Status != payentity.PaymentExpired {
		t.Fatalf("status = %v, want EXPIRED", out.Status)
	}
	if closeCalls != 1 {
		t.Fatalf("channel close called %d times, want 1", closeCalls)
	}
}

func TestListNeverTouchesTheChannel(t *testing.T) {
	var resolveHits int
	gw := &scriptGateway{resolveHits: &resolveHits}
	svc, _, _ := newTestPaymentService(t, gw)
	if _, err := svc.List(context.Background(), filtering.Filter{}, orderByDefault(), 20, 0); err != nil {
		t.Fatalf("list: %v", err)
	}
	if resolveHits != 0 {
		t.Fatalf("list resolved the gateway %d times — list reads never call the channel", resolveHits)
	}
}

func TestQueryTerminalIsNoOp(t *testing.T) {
	var resolveHits int
	gw := &scriptGateway{resolveHits: &resolveHits}
	svc, repo, merchantID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	repo.p[p.ID].Status = payentity.PaymentPaid
	if _, err := svc.Query(context.Background(), p.ID); err != nil {
		t.Fatalf("query: %v", err)
	}
	if resolveHits != 0 {
		t.Fatal("terminal payment touched the channel")
	}
}

func TestCloseOnPaidIsNoOp(t *testing.T) {
	var closeCalls int
	gw := &scriptGateway{closeCalled: &closeCalls}
	svc, repo, merchantID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	repo.p[p.ID].Status = payentity.PaymentPaid
	out, err := svc.Close(context.Background(), p.ID, 7)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v — late close must be a no-op", out.Status)
	}
	if closeCalls != 0 {
		t.Fatal("late close touched the channel")
	}
}

func TestCloseChannelAlreadyPaidFlipsToPaid(t *testing.T) {
	gw := &scriptGateway{
		closeErr: ErrChannelAlreadyPaid,
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-flip",
			PaidAt:         time.Now(),
		},
	}
	svc, _, merchantID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	out, err := svc.Close(context.Background(), p.ID, 7)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-flip" {
		t.Fatalf("flipped payment = %+v", out)
	}
}

func TestApplyChannelStateDuplicateAndOutOfOrder(t *testing.T) {
	svc, repo, merchantID := newTestPaymentService(t, &scriptGateway{})
	p, _ := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	// First success advances.
	advanced, err := svc.ApplyChannelState(ctx(), p.ID, ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-1", PayerID: "o-1",
	})
	if err != nil || !advanced {
		t.Fatalf("advanced = %v, err = %v", advanced, err)
	}
	// Duplicate delivery: no-op, no error.
	advanced, err = svc.ApplyChannelState(ctx(), p.ID, ChannelPaymentState{State: payentity.PaymentPaid})
	if err != nil || advanced {
		t.Fatalf("duplicate advanced = %v, err = %v", advanced, err)
	}
	// Out-of-order close receipt after success: still a no-op.
	advanced, err = svc.ApplyChannelState(ctx(), p.ID, ChannelPaymentState{State: payentity.PaymentClosed})
	if err != nil || advanced {
		t.Fatalf("out-of-order advanced = %v, err = %v", advanced, err)
	}
	stored, _ := repo.FindByID(context.Background(), p.ID)
	if stored.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v — terminal states are irreversible", stored.Status)
	}
}

func TestApplyChannelStateBackfillsEvidence(t *testing.T) {
	svc, repo, merchantID := newTestPaymentService(t, &scriptGateway{})
	p, _ := svc.Initiate(context.Background(), initiateInput(merchantID), 7)
	// First confirmation carries only the trade number.
	if _, err := svc.ApplyChannelState(ctx(), p.ID, ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-1",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// A later confirmation backfills the payer evidence.
	if _, err := svc.ApplyChannelState(ctx(), p.ID, ChannelPaymentState{
		State: payentity.PaymentPaid, PayerID: "openid-1",
	}); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	stored, _ := repo.FindByID(context.Background(), p.ID)
	if stored.ChannelTradeNo != "wx-txn-1" || stored.PayerID != "openid-1" {
		t.Fatalf("payment = %+v", stored)
	}
}
