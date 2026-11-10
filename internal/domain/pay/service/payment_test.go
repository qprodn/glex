package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/pay/crypt"
	"glex/pkg/listq"

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

// FindByBizNo returns the one Payment for the Pay Application and
// Business Order Number. A second row for the pair is a broken fixture.
func (f *fakePaymentRepo) FindByBizNo(ctx context.Context, req payrepo.FindByBizNoRequest) (*payentity.Payment, error) {
	var found *payentity.Payment
	for _, p := range f.p {
		if p.ApplicationID == req.ApplicationID && p.BizNo == req.BizNo {
			cp := *p
			found = &cp
		}
	}
	if found == nil {
		return nil, pay.ErrPaymentNotFound
	}
	return found, nil
}

func (f *fakePaymentRepo) List(context.Context, listq.Query) ([]*payentity.Payment, error) {
	return nil, nil
}

// ListPendingExpired mirrors the infra scan: still-Pending rows past their
// expiry, id ascending from afterID, bounded.
func (f *fakePaymentRepo) ListPendingExpired(ctx context.Context, req payrepo.ListPendingExpiredRequest) ([]*payentity.Payment, error) {
	ids := make([]int64, 0, len(f.p))
	for id := range f.p {
		if id > req.AfterID {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []*payentity.Payment
	for _, id := range ids {
		p := f.p[id]
		if p.Status == payentity.PaymentPending && p.ExpireTime.Before(req.Before) {
			cp := *p
			out = append(out, &cp)
			if len(out) >= req.Limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakePaymentRepo) Create(ctx context.Context, p *payentity.Payment) (*payentity.Payment, error) {
	cp := *p
	f.p[p.ID] = &cp
	return &cp, nil
}

func (f *fakePaymentRepo) SetChannelPayload(ctx context.Context, req payrepo.SetChannelPayloadRequest) error {
	f.p[req.ID].ChannelPayload = req.Payload
	f.resolved++
	return nil
}

func (f *fakePaymentRepo) SetFailReason(ctx context.Context, req payrepo.SetFailReasonRequest) error {
	f.p[req.ID].FailReason = req.Reason
	return nil
}

func (f *fakePaymentRepo) MarkPaid(ctx context.Context, req payrepo.MarkPaidRequest) (bool, error) {
	p, ok := f.p[req.ID]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentPaid
	p.ChannelTradeNo = req.Capture.ChannelTradeNo
	p.PayerID = req.Capture.PayerID
	if !req.Capture.PaidAt.IsZero() {
		paidAt := req.Capture.PaidAt
		p.PaidAt = &paidAt
	}
	return true, nil
}

func (f *fakePaymentRepo) BackfillPaidInfo(ctx context.Context, req payrepo.BackfillPaidInfoRequest) (bool, error) {
	p, ok := f.p[req.ID]
	if !ok || p.Status != payentity.PaymentPaid {
		return false, nil
	}
	changed := false
	if req.Capture.ChannelTradeNo != "" && p.ChannelTradeNo == "" {
		p.ChannelTradeNo = req.Capture.ChannelTradeNo
		changed = true
	}
	if req.Capture.PayerID != "" && p.PayerID == "" {
		p.PayerID = req.Capture.PayerID
		changed = true
	}
	return changed, nil
}

func (f *fakePaymentRepo) MarkFailed(ctx context.Context, req payrepo.MarkPaymentFailedRequest) (bool, error) {
	p, ok := f.p[req.ID]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentFailed
	p.FailReason = req.Reason
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

// addRefunded mirrors the refunded-sum advance the real MarkSuccess runs
// inside its transaction (the fake pair wires it through the refund repo).
func (f *fakePaymentRepo) addRefunded(id, amount int64) {
	if p, ok := f.p[id]; ok && p.Status == payentity.PaymentPaid {
		p.RefundedAmount += amount
	}
}

func (f *fakePaymentRepo) NextID() int64 { f.nextID++; return f.nextID }

// scriptGateway is the contract-test gateway: every behavior the payment
// tests need, nothing more.
type scriptGateway struct {
	prepayErr   error
	prepayBody  string
	prepayNo    string
	queryState  ChannelPaymentState
	queryErr    error
	closeErr    error
	closeCalled *int
	resolveHits *int
	// paidAfterClose, when set, is returned by every Query after the
	// first Close attempt — scripting the Query → Close(already paid) →
	// re-Query flip the settle and close paths run.
	paidAfterClose *ChannelPaymentState
	closeSeen      bool
}

func (g *scriptGateway) Channel() payentity.Channel { return payentity.ChannelWechat }

func (g *scriptGateway) Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error) {
	if g.resolveHits != nil {
		*g.resolveHits++
	}
	g.prepayNo = req.PaymentNo
	if g.prepayErr != nil {
		return PrepayResult{}, g.prepayErr
	}
	return PrepayResult{ChannelPayload: g.prepayBody}, nil
}

func (g *scriptGateway) Query(ctx context.Context, req QueryRequest) (ChannelPaymentState, error) {
	if g.resolveHits != nil {
		*g.resolveHits++
	}
	if g.queryErr != nil {
		return ChannelPaymentState{}, g.queryErr
	}
	if g.paidAfterClose != nil && g.closeSeen {
		return *g.paidAfterClose, nil
	}
	return g.queryState, nil
}

func (g *scriptGateway) Close(ctx context.Context, req CloseRequest) error {
	if g.closeCalled != nil {
		*g.closeCalled++
	}
	g.closeSeen = true
	return g.closeErr
}

func (g *scriptGateway) Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}

func (g *scriptGateway) QueryRefund(ctx context.Context, req RefundQueryRequest) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}

func (g *scriptGateway) ParseNotification(ctx context.Context, req NotificationRequest) (payentity.ChannelNotification, error) {
	return payentity.ChannelNotification{}, nil
}

func (g *scriptGateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error { return nil }

func (g *scriptGateway) AckSuccess() string { return "" }

// scriptGateways feeds the registry port a single scriptable gateway.
type scriptGateways struct {
	gw ChannelGateway
}

func (s *scriptGateways) Resolve(ctx context.Context, m *payentity.MerchantAccount) (ChannelGateway, error) {
	return s.gw, nil
}

func (s *scriptGateways) Invalidate(merchantID int64) {}

// payFixture is the in-memory payment service plus the rows a test
// reaches through the repository ports.
type payFixture struct {
	svc           Payment
	payments      *fakePaymentRepo
	apps          *fakePayApplicationRepo
	merchants     *fakeMerchantRepo
	notices       *memNotices
	completed     *memPublisher
	applicationID int64
	merchantID    int64
}

// newTestPaymentService builds the payment service over fakes and returns
// the Pay Application id callers pass to Initiate.
func newTestPaymentService(t *testing.T, gw *scriptGateway) (Payment, *fakePaymentRepo, int64) {
	t.Helper()
	fix := newPayFixture(t, gw)
	return fix.svc, fix.payments, fix.applicationID
}

// newPayFixture builds the payment service over fakes. The Pay Application
// is enabled and bound to the enabled WeChat merchant. Its payment result
// address is the literal the copy tests expect.
func newPayFixture(t *testing.T, gw *scriptGateway) payFixture {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	cipher := crypt.New()
	repo := newFakePaymentRepo()
	repo.nextID = 500
	merchantRepo := newFakeMerchantRepo(cipher)
	catalog := &fakeCatalog{schemas: []payentity.ChannelSchema{wechatSchema()}}
	merchantSvc := NewMerchantService(merchantRepo, catalog, cipher, &scriptGateways{}, slog.New(slog.DiscardHandler))
	m, err := merchantSvc.Create(context.Background(), wechatInput(7))
	if err != nil {
		t.Fatalf("create merchant fixture: %v", err)
	}
	apps := newFakePayApplicationRepo()
	app, err := apps.Create(context.Background(), &payentity.PayApplication{
		ID:             apps.NextID(),
		Name:           "收银台",
		MerchantID:     m.ID,
		Status:         payentity.ApplicationEnabled,
		OrderNotifyURL: "https://shop.example/paid",
		CreateBy:       7,
		UpdateBy:       7,
	})
	if err != nil {
		t.Fatalf("create application fixture: %v", err)
	}
	notices := &memNotices{}
	completed := &memPublisher{}
	svc := NewPaymentService(repo, apps, merchantRepo, &scriptGateways{gw: gw}, notices, completed, slog.New(slog.DiscardHandler))
	return payFixture{
		svc: svc, payments: repo, apps: apps, merchants: merchantRepo,
		notices: notices, completed: completed,
		applicationID: app.ID, merchantID: m.ID,
	}
}

func ctx() context.Context { return context.Background() }

func orderByDefault() ordering.OrderBy {
	var o ordering.OrderBy
	o.Fields = []ordering.Field{{Path: "id", Desc: true}}
	return o
}

func initiateInput(applicationID, actorID int64) CreatePaymentRequest {
	return CreatePaymentRequest{
		ApplicationID: applicationID,
		BizNo:         "ORDER-1",
		Subject:       "测试商品",
		Amount:        100,
		PayMethod:     payentity.PayMethodNative,
		ActorID:       actorID,
	}
}

func TestInitiateSameTermsReturnsTheExistingPayment(t *testing.T) {
	svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{prepayBody: "wxp://qr"})
	firstIn := initiateInput(applicationID, 7)
	firstIn.Subject = "第一笔标题"
	firstIn.ExpireMinutes = 15
	first, err := svc.Initiate(context.Background(), firstIn)
	if err != nil {
		t.Fatalf("first initiate: %v", err)
	}
	if first.Status != payentity.PaymentPending || first.Channel != payentity.ChannelWechat {
		t.Fatalf("first payment = %+v", first)
	}
	// Same Pay Application, Business Order Number, amount, and Pay Method
	// returns that Payment. Subject and expiry stay as first recorded.
	again := initiateInput(applicationID, 8)
	again.Subject = "另一个标题"
	again.ExpireMinutes = 60
	second, err := svc.Initiate(context.Background(), again)
	if err != nil {
		t.Fatalf("second initiate: %v", err)
	}
	firstAfter, _ := repo.FindByID(context.Background(), first.ID)
	if firstAfter.Status != payentity.PaymentPending {
		t.Fatalf("existing payment status = %v, want pending", firstAfter.Status)
	}
	if second.ID != first.ID || second.Subject != "第一笔标题" || second.CreateBy != 7 {
		t.Fatalf("second payment = %+v", second)
	}
	if !second.ExpireTime.Equal(first.ExpireTime) {
		t.Fatalf("expiry changed from %v to %v", first.ExpireTime, second.ExpireTime)
	}
	if len(repo.p) != 1 {
		t.Fatalf("payments stored = %d, want 1", len(repo.p))
	}
}

func TestInitiateRefusesDifferentAmountOrPayMethod(t *testing.T) {
	svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{})
	first, err := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	if err != nil {
		t.Fatalf("first initiate: %v", err)
	}
	differentAmount := initiateInput(applicationID, 8)
	differentAmount.Amount = 250
	if _, err := svc.Initiate(context.Background(), differentAmount); !errors.Is(err, pay.ErrPaymentConflict) {
		t.Fatalf("err = %v, want ErrPaymentConflict", err)
	}
	differentMethod := initiateInput(applicationID, 8)
	differentMethod.PayMethod = payentity.PayMethodJSAPI
	if _, err := svc.Initiate(context.Background(), differentMethod); !errors.Is(err, pay.ErrPaymentConflict) {
		t.Fatalf("err = %v, want ErrPaymentConflict", err)
	}
	stored, _ := repo.FindByID(context.Background(), first.ID)
	if stored.Amount != 100 || stored.PayMethod != payentity.PayMethodNative || stored.Status != payentity.PaymentPending {
		t.Fatalf("stored payment changed: %+v", stored)
	}
	if len(repo.p) != 1 {
		t.Fatalf("payments stored = %d, want 1", len(repo.p))
	}
}

func TestInitiateTerminalPaymentKeepsBusinessOrderNumber(t *testing.T) {
	cases := []struct {
		name   string
		status payentity.PaymentStatus
	}{
		{"closed", payentity.PaymentClosed},
		{"expired", payentity.PaymentExpired},
		{"failed", payentity.PaymentFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{})
			first, err := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
			if err != nil {
				t.Fatalf("initiate: %v", err)
			}
			switch status {
			case payentity.PaymentClosed:
				if _, err := repo.MarkClosed(context.Background(), first.ID); err != nil {
					t.Fatalf("close: %v", err)
				}
			case payentity.PaymentExpired:
				if _, err := repo.MarkExpired(context.Background(), first.ID); err != nil {
					t.Fatalf("expire: %v", err)
				}
			case payentity.PaymentFailed:
				if _, err := repo.MarkFailed(context.Background(), payrepo.MarkPaymentFailedRequest{ID: first.ID, Reason: "channel rejected"}); err != nil {
					t.Fatalf("fail: %v", err)
				}
			}
			again, err := svc.Initiate(context.Background(), initiateInput(applicationID, 9))
			if err != nil {
				t.Fatalf("repeat: %v", err)
			}
			if again.ID != first.ID || again.Status != status {
				t.Fatalf("repeat = %+v, want id %d status %v", again, first.ID, status)
			}
			fresh := initiateInput(applicationID, 9)
			fresh.BizNo = "ORDER-2"
			second, err := svc.Initiate(context.Background(), fresh)
			if err != nil {
				t.Fatalf("new business order: %v", err)
			}
			if second.ID == first.ID || second.Status != payentity.PaymentPending || second.BizNo != "ORDER-2" {
				t.Fatalf("new payment = %+v", second)
			}
		})
	}
}

func TestInitiateTwoApplicationsShareABusinessOrderNumber(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{})
	other, err := fix.apps.Create(context.Background(), &payentity.PayApplication{
		ID:             fix.apps.NextID(),
		Name:           "另一家",
		MerchantID:     fix.merchantID,
		Status:         payentity.ApplicationEnabled,
		OrderNotifyURL: "",
		CreateBy:       7,
		UpdateBy:       7,
	})
	if err != nil {
		t.Fatalf("second application: %v", err)
	}
	first, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("first initiate: %v", err)
	}
	secondIn := initiateInput(other.ID, 7)
	second, err := fix.svc.Initiate(context.Background(), secondIn)
	if err != nil {
		t.Fatalf("second initiate: %v", err)
	}
	if first.ID == second.ID || first.ApplicationID != fix.applicationID || second.ApplicationID != other.ID {
		t.Fatalf("payments = %+v / %+v", first, second)
	}
	if first.MerchantID != fix.merchantID || second.MerchantID != fix.merchantID || first.Channel != payentity.ChannelWechat || second.Channel != payentity.ChannelWechat {
		t.Fatalf("snapshot = %+v / %+v, merchant %d", first, second, fix.merchantID)
	}
	if first.NotifyURL != "https://shop.example/paid" || second.NotifyURL != "" {
		t.Fatalf("notify urls = %q / %q", first.NotifyURL, second.NotifyURL)
	}
	got, err := fix.svc.GetByBizNo(context.Background(), GetByBizNoRequest{ApplicationID: fix.applicationID, BizNo: "ORDER-1"})
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if got.ID != first.ID {
		t.Fatalf("read = %+v, want %d", got, first.ID)
	}
	got, err = fix.svc.GetByBizNo(context.Background(), GetByBizNoRequest{ApplicationID: other.ID, BizNo: "ORDER-1"})
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("read = %+v, want %d", got, second.ID)
	}
	if _, err := fix.svc.GetByBizNo(context.Background(), GetByBizNoRequest{ApplicationID: fix.applicationID, BizNo: "ORDER-MISSING"}); !errors.Is(err, pay.ErrPaymentNotFound) {
		t.Fatalf("err = %v, want ErrPaymentNotFound", err)
	}
}

func TestInitiateRefusesDisabledOrWithdrawnApplicationOrMerchant(t *testing.T) {
	t.Run("disabled application", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{})
		fix.apps.m[fix.applicationID].Status = payentity.ApplicationDisabled
		if _, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7)); !errors.Is(err, pay.ErrApplicationInvalid) {
			t.Fatalf("err = %v, want ErrApplicationInvalid", err)
		}
	})
	t.Run("withdrawn application", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{})
		if err := fix.apps.Delete(context.Background(), payrepo.DeletePayApplicationRequest{ID: fix.applicationID, ActorID: 7}); err != nil {
			t.Fatalf("withdraw: %v", err)
		}
		if _, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7)); !errors.Is(err, pay.ErrApplicationNotFound) {
			t.Fatalf("err = %v, want ErrApplicationNotFound", err)
		}
	})
	t.Run("disabled merchant", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{})
		fix.merchants.m[fix.merchantID].Status = payentity.MerchantDisabled
		if _, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7)); !errors.Is(err, pay.ErrMerchantInvalid) {
			t.Fatalf("err = %v, want ErrMerchantInvalid", err)
		}
	})
	t.Run("withdrawn merchant", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{})
		fix.merchants.deleted[fix.merchantID] = true
		if _, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7)); !errors.Is(err, pay.ErrMerchantNotFound) {
			t.Fatalf("err = %v, want ErrMerchantNotFound", err)
		}
	})
	t.Run("existing payment still returns after the application is disabled", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{prepayBody: "wxp://same"})
		first, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
		if err != nil {
			t.Fatalf("initiate: %v", err)
		}
		fix.apps.m[fix.applicationID].Status = payentity.ApplicationDisabled
		again, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 8))
		if err != nil {
			t.Fatalf("repeat: %v", err)
		}
		if again.ID != first.ID || again.ChannelPayload != "" {
			t.Fatalf("repeat = %+v", again)
		}
		out, err := fix.svc.Prepay(context.Background(), PrepayPaymentRequest{ID: again.ID, NotifyBaseURL: "https://pay.example.com"})
		if err != nil {
			t.Fatalf("prepay: %v", err)
		}
		if out.ID != first.ID || out.ChannelPayload != "wxp://same" {
			t.Fatalf("prepay = %+v", out)
		}
	})
}

func TestPrepayFinishesOnTheSamePendingPayment(t *testing.T) {
	var hits int
	gw := &scriptGateway{prepayBody: "wxp://once", resolveHits: &hits}
	svc, _, applicationID := newTestPaymentService(t, gw)
	first, err := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	again, err := svc.Initiate(context.Background(), initiateInput(applicationID, 8))
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	out, err := svc.Prepay(context.Background(), PrepayPaymentRequest{ID: again.ID, NotifyBaseURL: "https://pay.example.com"})
	if err != nil {
		t.Fatalf("prepay: %v", err)
	}
	if out.ID != first.ID || out.ChannelPayload != "wxp://once" || gw.prepayNo != fmt.Sprintf("%d", first.ID) {
		t.Fatalf("prepay = %+v, channel order %q", out, gw.prepayNo)
	}
	if hits != 1 {
		t.Fatalf("channel calls = %d, want 1", hits)
	}
	repeated, err := svc.Prepay(context.Background(), PrepayPaymentRequest{ID: first.ID, NotifyBaseURL: "https://pay.example.com"})
	if err != nil {
		t.Fatalf("second prepay: %v", err)
	}
	if repeated.ChannelPayload != "wxp://once" || hits != 1 {
		t.Fatalf("second prepay payload = %q, channel calls = %d", repeated.ChannelPayload, hits)
	}
}

func TestInitiateValidations(t *testing.T) {
	svc, _, applicationID := newTestPaymentService(t, &scriptGateway{})
	in := initiateInput(applicationID, 7)
	in.Amount = 0
	if _, err := svc.Initiate(context.Background(), in); !errors.Is(err, pay.ErrPaymentInvalid) {
		t.Fatalf("err = %v, want ErrPaymentInvalid", err)
	}
	in = initiateInput(applicationID, 7)
	in.ApplicationID = 999999
	if _, err := svc.Initiate(context.Background(), in); !errors.Is(err, pay.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
	in = initiateInput(applicationID, 7)
	in.BizNo = ""
	if _, err := svc.Initiate(context.Background(), in); !errors.Is(err, pay.ErrPaymentInvalid) {
		t.Fatalf("err = %v, want ErrPaymentInvalid", err)
	}
}

func TestPrepayBackfillsPayload(t *testing.T) {
	svc, _, applicationID := newTestPaymentService(t, &scriptGateway{prepayBody: "wxp://faked"})
	p, err := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	out, err := svc.Prepay(context.Background(), PrepayPaymentRequest{ID: p.ID, NotifyBaseURL: "https://pay.example.com"})
	if err != nil {
		t.Fatalf("prepay: %v", err)
	}
	if out.ChannelPayload != "wxp://faked" {
		t.Fatalf("payload = %q", out.ChannelPayload)
	}
}

func TestPrepayChannelFailureFailsThePayment(t *testing.T) {
	svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{prepayErr: errors.New("ORDER_CLOSED by channel")})
	p, err := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	_, err = svc.Prepay(context.Background(), PrepayPaymentRequest{ID: p.ID})
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
	svc, repo, applicationID := newTestPaymentService(t, gw)
	p, err := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
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
	svc, repo, applicationID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
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

func TestGetSettlesExpiredPaymentPaidBetweenQueryAndClose(t *testing.T) {
	// The user paid between settle's Query and its Close: the close
	// answers "already paid" and the flip-to-query must land PAID, never
	// EXPIRED — captured money on a dead row is unrefundable.
	paid := ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-late",
		PayerID: "openid-late", PaidAt: time.Now(), Amount: 100,
	}
	gw := &scriptGateway{
		queryState:     ChannelPaymentState{State: payentity.PaymentPending},
		closeErr:       ErrChannelAlreadyPaid,
		paidAfterClose: &paid,
	}
	svc, repo, applicationID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	repo.p[p.ID].ExpireTime = time.Now().Add(-time.Minute)
	out, err := svc.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-late" {
		t.Fatalf("settled payment = %+v, want PAID with the channel trade no", out)
	}
}

func TestListNeverTouchesTheChannel(t *testing.T) {
	var resolveHits int
	gw := &scriptGateway{resolveHits: &resolveHits}
	svc, _, _ := newTestPaymentService(t, gw)
	if _, err := svc.List(context.Background(), listq.Query{OrderBy: orderByDefault(), Limit: 20}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if resolveHits != 0 {
		t.Fatalf("list resolved the gateway %d times — list reads never call the channel", resolveHits)
	}
}

func TestQueryTerminalIsNoOp(t *testing.T) {
	var resolveHits int
	gw := &scriptGateway{resolveHits: &resolveHits}
	svc, repo, applicationID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
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
	svc, repo, applicationID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	repo.p[p.ID].Status = payentity.PaymentPaid
	out, err := svc.Close(context.Background(), ClosePaymentRequest{ID: p.ID, ActorID: 7})
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
	svc, _, applicationID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	out, err := svc.Close(context.Background(), ClosePaymentRequest{ID: p.ID, ActorID: 7})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-flip" {
		t.Fatalf("flipped payment = %+v", out)
	}
}

func TestApplyChannelStateDuplicateAndOutOfOrder(t *testing.T) {
	svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{})
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	// First success advances.
	advanced, err := svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-1", PayerID: "o-1",
	}})
	if err != nil || !advanced {
		t.Fatalf("advanced = %v, err = %v", advanced, err)
	}
	// Duplicate delivery: no-op, no error.
	advanced, err = svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{State: payentity.PaymentPaid}})
	if err != nil || advanced {
		t.Fatalf("duplicate advanced = %v, err = %v", advanced, err)
	}
	// Out-of-order close receipt after success: still a no-op.
	advanced, err = svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{State: payentity.PaymentClosed}})
	if err != nil || advanced {
		t.Fatalf("out-of-order advanced = %v, err = %v", advanced, err)
	}
	stored, _ := repo.FindByID(context.Background(), p.ID)
	if stored.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v — terminal states are irreversible", stored.Status)
	}
}

func TestApplyChannelStateBackfillsEvidence(t *testing.T) {
	svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{})
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	// First confirmation carries only the trade number.
	if _, err := svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-1",
	}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// A later confirmation backfills the payer evidence.
	if _, err := svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{
		State: payentity.PaymentPaid, PayerID: "openid-1",
	}}); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	stored, _ := repo.FindByID(context.Background(), p.ID)
	if stored.ChannelTradeNo != "wx-txn-1" || stored.PayerID != "openid-1" {
		t.Fatalf("payment = %+v", stored)
	}
}

func TestApplyChannelStateRejectsAmountMismatch(t *testing.T) {
	svc, repo, applicationID := newTestPaymentService(t, &scriptGateway{})
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	// A signature-valid success naming the wrong amount never settles.
	advanced, err := svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-1", Amount: 999,
	}})
	if err != nil || advanced {
		t.Fatalf("mismatch advanced = %v, err = %v", advanced, err)
	}
	stored, _ := repo.FindByID(context.Background(), p.ID)
	if stored.Status != payentity.PaymentPending {
		t.Fatalf("status = %v — a mismatched amount must not settle", stored.Status)
	}
	// The verified amount settles.
	advanced, err = svc.ApplyChannelState(ctx(), ApplyChannelStateRequest{PaymentNo: p.ID, State: ChannelPaymentState{
		State: payentity.PaymentPaid, ChannelTradeNo: "wx-txn-1", Amount: 100,
	}})
	if err != nil || !advanced {
		t.Fatalf("verified advanced = %v, err = %v", advanced, err)
	}
	stored, _ = repo.FindByID(context.Background(), p.ID)
	if stored.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v, want Paid", stored.Status)
	}
}

// newFakeCipher builds the real credential cipher over the shared test
// master key (the environment is set by the service test constructors).
func newFakeCipher() *crypt.Cipher {
	return crypt.New()
}

func TestSweepExpiredSettlesOnlyExpiredPending(t *testing.T) {
	var closeCalls int
	gw := &scriptGateway{
		queryState:  ChannelPaymentState{State: payentity.PaymentPending},
		closeCalled: &closeCalls,
	}
	svc, repo, applicationID := newTestPaymentService(t, gw)
	inA := initiateInput(applicationID, 7)
	inA.BizNo = "ORDER-A"
	inB := initiateInput(applicationID, 7)
	inB.BizNo = "ORDER-B"
	inL := initiateInput(applicationID, 7)
	inL.BizNo = "ORDER-LIVE"
	expiredA, _ := svc.Initiate(context.Background(), inA)
	expiredB, _ := svc.Initiate(context.Background(), inB)
	live, _ := svc.Initiate(context.Background(), inL)
	repo.p[expiredA.ID].ExpireTime = time.Now().Add(-time.Hour)
	repo.p[expiredB.ID].ExpireTime = time.Now().Add(-time.Minute)
	// `live` keeps its future expiry.

	settled, err := svc.SweepExpired(context.Background(), 50)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if settled != 2 {
		t.Fatalf("settled = %d, want 2", settled)
	}
	for _, id := range []int64{expiredA.ID, expiredB.ID} {
		if p := repo.p[id]; p.Status != payentity.PaymentExpired {
			t.Fatalf("payment %d status = %v, want EXPIRED", id, p.Status)
		}
	}
	if p := repo.p[live.ID]; p.Status != payentity.PaymentPending {
		t.Fatalf("unexpired payment was swept: status = %v", p.Status)
	}
	if closeCalls != 2 {
		t.Fatalf("channel close called %d times, want 2", closeCalls)
	}
}

func TestSweepExpiredContinuesPastFailure(t *testing.T) {
	gw := &scriptGateway{
		queryState: ChannelPaymentState{State: payentity.PaymentPending},
	}
	svc, repo, applicationID := newTestPaymentService(t, gw)
	good, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	repo.p[good.ID].ExpireTime = time.Now().Add(-time.Hour)

	// A candidate whose merchant no longer resolves — settle fails for it,
	// the batch must still settle the healthy row.
	bad := &payentity.Payment{
		ID: 999999, MerchantID: 424242, Channel: payentity.ChannelWechat,
		BizNo: "b", Subject: "s", Amount: 100, Status: payentity.PaymentPending,
		ExpireTime: time.Now().Add(-time.Hour),
	}
	repo.p[bad.ID] = bad

	settled, err := svc.SweepExpired(context.Background(), 50)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if settled != 1 {
		t.Fatalf("settled = %d, want 1", settled)
	}
	if p := repo.p[good.ID]; p.Status != payentity.PaymentExpired {
		t.Fatalf("healthy candidate not settled: %v", p.Status)
	}
}

func TestSweepExpiredPagesPastPoisonedHead(t *testing.T) {
	gw := &scriptGateway{queryState: ChannelPaymentState{State: payentity.PaymentPending}}
	svc, repo, applicationID := newTestPaymentService(t, gw)
	// A healthy expired row sits behind two poisoned ones (unresolvable
	// merchant); with limit=2 the first batch is fully poisoned and the
	// cursor must page the run forward to the healthy row.
	good, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	repo.p[good.ID].ExpireTime = time.Now().Add(-time.Hour)
	for i, id := range []int64{999001, 999002} {
		repo.p[id] = &payentity.Payment{
			ID: id, MerchantID: 424242, Channel: payentity.ChannelWechat,
			BizNo: fmt.Sprintf("bad-%d", i), Subject: "s", Amount: 100,
			Status: payentity.PaymentPending, ExpireTime: time.Now().Add(-time.Hour),
		}
	}
	settled, err := svc.SweepExpired(context.Background(), 2)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if settled != 1 {
		t.Fatalf("settled = %d, want 1 (the healthy row behind the poisoned head)", settled)
	}
	if p := repo.p[good.ID]; p.Status != payentity.PaymentExpired {
		t.Fatalf("healthy row not settled: %v", p.Status)
	}
}

func TestGetExpiredPendingPaidRecordsNoticeWithoutPaymentCompleted(t *testing.T) {
	paidAt := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	gw := &scriptGateway{
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-77",
			PayerID:        "openid-77",
			PaidAt:         paidAt,
			Amount:         100,
		},
	}
	fix := newPayFixture(t, gw)
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
	before := time.Now()
	out, err := fix.svc.Get(context.Background(), p.ID)
	after := time.Now()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-77" {
		t.Fatalf("settled payment = %+v, want paid", out)
	}
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d Payment Completed events, want 0", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != p.ID || notice.RefundID != 0 ||
		notice.BizNo != "ORDER-1" || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for ORDER-1", notice)
	}
}

func TestGetByBizNoExpiredPendingPaidRecordsNoticeWithoutPaymentCompleted(t *testing.T) {
	gw := &scriptGateway{
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-biz",
			PaidAt:         time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
			Amount:         100,
		},
	}
	fix := newPayFixture(t, gw)
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
	before := time.Now()
	out, err := fix.svc.GetByBizNo(context.Background(), GetByBizNoRequest{
		ApplicationID: fix.applicationID, BizNo: "ORDER-1",
	})
	after := time.Now()
	if err != nil {
		t.Fatalf("get by biz no: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ID != p.ID {
		t.Fatalf("settled payment = %+v, want paid %d", out, p.ID)
	}
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d Payment Completed events, want 0", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != p.ID || notice.RefundID != 0 ||
		notice.BizNo != "ORDER-1" || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for ORDER-1", notice)
	}
}

func TestQueryPendingPaidRecordsNoticeWithoutPaymentCompleted(t *testing.T) {
	gw := &scriptGateway{
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-query",
			PaidAt:         time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
			Amount:         100,
		},
	}
	fix := newPayFixture(t, gw)
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if !p.ExpireTime.After(time.Now()) {
		t.Fatal("fixture payment is already expired; query must settle a live pending payment")
	}
	before := time.Now()
	out, err := fix.svc.Query(context.Background(), p.ID)
	after := time.Now()
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-query" {
		t.Fatalf("settled payment = %+v, want paid", out)
	}
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d Payment Completed events, want 0", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != p.ID || notice.RefundID != 0 ||
		notice.BizNo != "ORDER-1" || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for ORDER-1", notice)
	}
}

func TestSweepExpiredPaidRecordsNoticeWithoutPaymentCompleted(t *testing.T) {
	gw := &scriptGateway{
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-sweep",
			PaidAt:         time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC),
			Amount:         100,
		},
	}
	fix := newPayFixture(t, gw)
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
	before := time.Now()
	settled, err := fix.svc.SweepExpired(context.Background(), 50)
	after := time.Now()
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if settled != 1 {
		t.Fatalf("settled = %d, want 1", settled)
	}
	stored := fix.payments.p[p.ID]
	if stored.Status != payentity.PaymentPaid || stored.ChannelTradeNo != "wx-txn-sweep" {
		t.Fatalf("swept payment = %+v, want paid", stored)
	}
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d Payment Completed events, want 0", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != p.ID || notice.RefundID != 0 ||
		notice.BizNo != "ORDER-1" || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for ORDER-1", notice)
	}
}

func TestCloseAlreadyPaidRecordsNoticeWithoutPaymentCompleted(t *testing.T) {
	gw := &scriptGateway{
		closeErr: ErrChannelAlreadyPaid,
		queryState: ChannelPaymentState{
			State:          payentity.PaymentPaid,
			ChannelTradeNo: "wx-txn-flip",
			PaidAt:         time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC),
			Amount:         100,
		},
	}
	fix := newPayFixture(t, gw)
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	before := time.Now()
	out, err := fix.svc.Close(context.Background(), ClosePaymentRequest{ID: p.ID, ActorID: 7})
	after := time.Now()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-flip" {
		t.Fatalf("flipped payment = %+v, want paid", out)
	}
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d Payment Completed events, want 0", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != p.ID || notice.RefundID != 0 ||
		notice.BizNo != "ORDER-1" || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for ORDER-1", notice)
	}
}

func TestSweepExpiredStopsOnDeadContext(t *testing.T) {
	gw := &scriptGateway{queryState: ChannelPaymentState{State: payentity.PaymentPending}}
	svc, repo, applicationID := newTestPaymentService(t, gw)
	p, _ := svc.Initiate(context.Background(), initiateInput(applicationID, 7))
	repo.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.SweepExpired(ctx, 50); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled surfaced, not masked as success", err)
	}
}

func assertNoPaymentCompleted(t *testing.T, fix payFixture) {
	t.Helper()
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d Payment Completed events, want 0", len(fix.completed.sent))
	}
}

func assertOneWaitingPaymentNotice(t *testing.T, fix payFixture, paymentID int64, bizNo string, before, after time.Time) {
	t.Helper()
	assertNoPaymentCompleted(t, fix)
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != paymentID || notice.RefundID != 0 ||
		notice.BizNo != bizNo || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for %s", notice, bizNo)
	}
}

func paidChannelState(tradeNo string) ChannelPaymentState {
	return ChannelPaymentState{
		State:          payentity.PaymentPaid,
		ChannelTradeNo: tradeNo,
		PaidAt:         time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC),
		Amount:         100,
	}
}

func TestEmptyCopiedAddressRecordsNoNoticeAfterApplicationAddressIsFilled(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{queryState: paidChannelState("wx-txn-empty")})
	fix.apps.m[fix.applicationID].OrderNotifyURL = ""
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if p.NotifyURL != "" {
		t.Fatalf("copied address = %q, want empty", p.NotifyURL)
	}
	fix.apps.m[fix.applicationID].OrderNotifyURL = "https://shop.example/later"
	out, err := fix.svc.Query(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.NotifyURL != "" {
		t.Fatalf("payment = %+v, want paid with the empty copied address", out)
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %+v, want none when the copied address is empty", fix.notices.rows)
	}
	assertNoPaymentCompleted(t, fix)
}

func TestPaidNoticeInsertFailureDoesNotFailSettlement(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{queryState: paidChannelState("wx-txn-degraded")})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.notices.err = errors.New("store down")
	out, err := fix.svc.Query(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("query: %v, want the paid settlement kept", err)
	}
	if out.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v, want paid", out.Status)
	}
	if fix.notices.inserts != 1 || len(fix.notices.rows) != 0 {
		t.Fatalf("inserts = %d rows = %d, want one failed insert and no row", fix.notices.inserts, len(fix.notices.rows))
	}
	assertNoPaymentCompleted(t, fix)
}

func TestDuplicatePaidNoticeInsertIsNotAnError(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{queryState: paidChannelState("wx-txn-dup")})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.notices.rows = append(fix.notices.rows, &payentity.BusinessNotice{
		ID: 1, ApplicationID: fix.applicationID, Kind: payentity.NoticeKindPayment,
		PaymentID: p.ID, BizNo: "ORDER-1", NotifyURL: "https://already.example/paid",
		Status: payentity.NoticeWaiting,
	})
	out, err := fix.svc.Query(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("query: %v, want duplicate notice insert ignored", err)
	}
	if out.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v, want paid", out.Status)
	}
	if fix.notices.inserts != 1 || len(fix.notices.rows) != 1 || fix.notices.rows[0].NotifyURL != "https://already.example/paid" {
		t.Fatalf("notices = %+v inserts = %d, want the original notice kept", fix.notices.rows, fix.notices.inserts)
	}
	assertNoPaymentCompleted(t, fix)
}

func TestQueryAgainDoesNotRecordASecondNotice(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{queryState: paidChannelState("wx-txn-once")})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if _, err := fix.svc.Query(context.Background(), p.ID); err != nil {
		t.Fatalf("first query: %v", err)
	}
	if _, err := fix.svc.Query(context.Background(), p.ID); err != nil {
		t.Fatalf("second query: %v", err)
	}
	if len(fix.notices.rows) != 1 || fix.notices.inserts != 1 {
		t.Fatalf("notices = %d inserts = %d, want one notice from the first paid transition", len(fix.notices.rows), fix.notices.inserts)
	}
	assertNoPaymentCompleted(t, fix)
}

func TestApplyChannelStatePaidDoesNotRecordNotice(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	advanced, err := fix.svc.ApplyChannelState(context.Background(), ApplyChannelStateRequest{
		PaymentNo: p.ID,
		State:     paidChannelState("wx-txn-apply"),
	})
	if err != nil || !advanced {
		t.Fatalf("advanced = %v, err = %v", advanced, err)
	}
	stored, _ := fix.payments.FindByID(context.Background(), p.ID)
	if stored.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v, want paid", stored.Status)
	}
	if len(fix.notices.rows) != 0 || fix.notices.inserts != 0 {
		t.Fatalf("notices = %d inserts = %d, want ApplyChannelState to leave the notice to its caller", len(fix.notices.rows), fix.notices.inserts)
	}
	assertNoPaymentCompleted(t, fix)
}

func TestGetLivePendingDoesNotRecordNotice(t *testing.T) {
	var hits int
	fix := newPayFixture(t, &scriptGateway{queryState: paidChannelState("wx-txn-live"), resolveHits: &hits})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	out, err := fix.svc.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Status != payentity.PaymentPending || hits != 0 {
		t.Fatalf("payment = %+v, channel hits = %d, want a live pending row and no channel call", out, hits)
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %d, want 0", len(fix.notices.rows))
	}
	assertNoPaymentCompleted(t, fix)
}

func TestGetExpiredPaidBetweenQueryAndCloseRecordsNotice(t *testing.T) {
	paid := paidChannelState("wx-txn-late")
	fix := newPayFixture(t, &scriptGateway{
		queryState:     ChannelPaymentState{State: payentity.PaymentPending},
		closeErr:       ErrChannelAlreadyPaid,
		paidAfterClose: &paid,
	})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Minute)
	before := time.Now()
	out, err := fix.svc.Get(context.Background(), p.ID)
	after := time.Now()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Status != payentity.PaymentPaid || out.ChannelTradeNo != "wx-txn-late" {
		t.Fatalf("settled payment = %+v, want paid", out)
	}
	assertOneWaitingPaymentNotice(t, fix, p.ID, "ORDER-1", before, after)
}

func TestSweepExpiredPaidRecordsOneNoticeEach(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{queryState: paidChannelState("wx-txn-batch")})
	inA := initiateInput(fix.applicationID, 7)
	inA.BizNo = "ORDER-A"
	inB := initiateInput(fix.applicationID, 7)
	inB.BizNo = "ORDER-B"
	a, err := fix.svc.Initiate(context.Background(), inA)
	if err != nil {
		t.Fatalf("initiate a: %v", err)
	}
	b, err := fix.svc.Initiate(context.Background(), inB)
	if err != nil {
		t.Fatalf("initiate b: %v", err)
	}
	fix.payments.p[a.ID].ExpireTime = time.Now().Add(-time.Hour)
	fix.payments.p[b.ID].ExpireTime = time.Now().Add(-time.Minute)
	settled, err := fix.svc.SweepExpired(context.Background(), 50)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if settled != 2 || len(fix.notices.rows) != 2 {
		t.Fatalf("settled = %d notices = %d, want 2 and 2", settled, len(fix.notices.rows))
	}
	got := map[int64]string{}
	for _, notice := range fix.notices.rows {
		if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
			notice.NotifyURL != "https://shop.example/paid" || notice.AttemptCount != 0 || notice.RefundID != 0 {
			t.Fatalf("notice = %+v", notice)
		}
		got[notice.PaymentID] = notice.BizNo
	}
	if got[a.ID] != "ORDER-A" || got[b.ID] != "ORDER-B" {
		t.Fatalf("notices = %+v, want one each for ORDER-A and ORDER-B", fix.notices.rows)
	}
	assertNoPaymentCompleted(t, fix)
}

func TestSettlementOtherThanPaidRecordsNoNotice(t *testing.T) {
	t.Run("read expires", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{queryState: ChannelPaymentState{State: payentity.PaymentPending}})
		p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
		if err != nil {
			t.Fatalf("initiate: %v", err)
		}
		fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Minute)
		out, err := fix.svc.Get(context.Background(), p.ID)
		if err != nil || out.Status != payentity.PaymentExpired {
			t.Fatalf("get = (%+v, %v), want expired", out, err)
		}
		if len(fix.notices.rows) != 0 {
			t.Fatalf("notices = %d, want 0", len(fix.notices.rows))
		}
		assertNoPaymentCompleted(t, fix)
	})
	for _, state := range []payentity.PaymentStatus{payentity.PaymentFailed, payentity.PaymentClosed, payentity.PaymentExpired} {
		t.Run(fmt.Sprintf("query %d", state), func(t *testing.T) {
			fix := newPayFixture(t, &scriptGateway{queryState: ChannelPaymentState{State: state, FailReason: "declined"}})
			p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
			if err != nil {
				t.Fatalf("initiate: %v", err)
			}
			out, err := fix.svc.Query(context.Background(), p.ID)
			if err != nil || out.Status != state {
				t.Fatalf("query = (%+v, %v), want status %d", out, err, state)
			}
			if len(fix.notices.rows) != 0 {
				t.Fatalf("notices = %d, want 0", len(fix.notices.rows))
			}
			assertNoPaymentCompleted(t, fix)
		})
	}
	t.Run("sweep expires", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{queryState: ChannelPaymentState{State: payentity.PaymentPending}})
		p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
		if err != nil {
			t.Fatalf("initiate: %v", err)
		}
		fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
		settled, err := fix.svc.SweepExpired(context.Background(), 50)
		if err != nil || settled != 1 || fix.payments.p[p.ID].Status != payentity.PaymentExpired {
			t.Fatalf("sweep = (%d, %v) status = %v, want one expired row", settled, err, fix.payments.p[p.ID].Status)
		}
		if len(fix.notices.rows) != 0 {
			t.Fatalf("notices = %d, want 0", len(fix.notices.rows))
		}
		assertNoPaymentCompleted(t, fix)
	})
	t.Run("close succeeds", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{})
		p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
		if err != nil {
			t.Fatalf("initiate: %v", err)
		}
		out, err := fix.svc.Close(context.Background(), ClosePaymentRequest{ID: p.ID, ActorID: 7})
		if err != nil || out.Status != payentity.PaymentClosed {
			t.Fatalf("close = (%+v, %v), want closed", out, err)
		}
		if len(fix.notices.rows) != 0 {
			t.Fatalf("notices = %d, want 0", len(fix.notices.rows))
		}
		assertNoPaymentCompleted(t, fix)
	})
	t.Run("close channel error", func(t *testing.T) {
		fix := newPayFixture(t, &scriptGateway{closeErr: errors.New("channel down")})
		p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
		if err != nil {
			t.Fatalf("initiate: %v", err)
		}
		out, err := fix.svc.Close(context.Background(), ClosePaymentRequest{ID: p.ID, ActorID: 7})
		if out != nil || !errors.Is(err, pay.ErrChannelError) {
			t.Fatalf("close = (%+v, %v), want channel error", out, err)
		}
		if fix.payments.p[p.ID].Status != payentity.PaymentPending || len(fix.notices.rows) != 0 {
			t.Fatalf("payment status = %v notices = %d, want pending and no notice", fix.payments.p[p.ID].Status, len(fix.notices.rows))
		}
		assertNoPaymentCompleted(t, fix)
	})
}

func TestStoppedApplicationOrMerchantStillSettlesPaidNotice(t *testing.T) {
	stops := []struct {
		name string
		stop func(t *testing.T, fix payFixture)
	}{
		{name: "disabled application", stop: func(t *testing.T, fix payFixture) {
			fix.apps.m[fix.applicationID].Status = payentity.ApplicationDisabled
		}},
		{name: "withdrawn application", stop: func(t *testing.T, fix payFixture) {
			if err := fix.apps.Delete(context.Background(), payrepo.DeletePayApplicationRequest{ID: fix.applicationID, ActorID: 7}); err != nil {
				t.Fatalf("withdraw application: %v", err)
			}
		}},
		{name: "disabled merchant", stop: func(t *testing.T, fix payFixture) {
			fix.merchants.m[fix.merchantID].Status = payentity.MerchantDisabled
		}},
		{name: "withdrawn merchant", stop: func(t *testing.T, fix payFixture) {
			fix.merchants.deleted[fix.merchantID] = true
		}},
	}
	paths := []struct {
		name   string
		expire bool
		settle func(t *testing.T, fix payFixture, id int64)
	}{
		{name: "read", expire: true, settle: func(t *testing.T, fix payFixture, id int64) {
			out, err := fix.svc.Get(context.Background(), id)
			if err != nil || out.Status != payentity.PaymentPaid {
				t.Fatalf("get = (%+v, %v), want paid", out, err)
			}
		}},
		{name: "lookup", expire: true, settle: func(t *testing.T, fix payFixture, id int64) {
			out, err := fix.svc.GetByBizNo(context.Background(), GetByBizNoRequest{ApplicationID: fix.applicationID, BizNo: "ORDER-1"})
			if err != nil || out.Status != payentity.PaymentPaid || out.ID != id {
				t.Fatalf("lookup = (%+v, %v), want paid %d", out, err, id)
			}
		}},
		{name: "query", settle: func(t *testing.T, fix payFixture, id int64) {
			out, err := fix.svc.Query(context.Background(), id)
			if err != nil || out.Status != payentity.PaymentPaid {
				t.Fatalf("query = (%+v, %v), want paid", out, err)
			}
		}},
		{name: "sweep", expire: true, settle: func(t *testing.T, fix payFixture, id int64) {
			settled, err := fix.svc.SweepExpired(context.Background(), 50)
			if err != nil || settled != 1 || fix.payments.p[id].Status != payentity.PaymentPaid {
				t.Fatalf("sweep = (%d, %v) status = %v, want one paid row", settled, err, fix.payments.p[id].Status)
			}
		}},
		{name: "close", settle: func(t *testing.T, fix payFixture, id int64) {
			out, err := fix.svc.Close(context.Background(), ClosePaymentRequest{ID: id, ActorID: 7})
			if err != nil || out.Status != payentity.PaymentPaid {
				t.Fatalf("close = (%+v, %v), want paid", out, err)
			}
		}},
	}
	for _, st := range stops {
		for _, path := range paths {
			t.Run(st.name+" "+path.name, func(t *testing.T) {
				fix := newPayFixture(t, &scriptGateway{
					closeErr:   ErrChannelAlreadyPaid,
					queryState: paidChannelState("wx-txn-stop"),
				})
				created, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
				if err != nil {
					t.Fatalf("initiate: %v", err)
				}
				if path.expire {
					fix.payments.p[created.ID].ExpireTime = time.Now().Add(-time.Hour)
				}
				st.stop(t, fix)
				before := time.Now()
				path.settle(t, fix, created.ID)
				after := time.Now()
				assertOneWaitingPaymentNotice(t, fix, created.ID, "ORDER-1", before, after)
			})
		}
	}
}
