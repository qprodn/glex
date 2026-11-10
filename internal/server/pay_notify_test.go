package server

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"glex/internal/application/pay"
	"glex/internal/conf"
	paydomain "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"
	"glex/internal/infra/pay/adapter/wechat"
	"glex/internal/infra/pay/crypt"
	"glex/pkg/listq"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// The notify route's gold-path integration test (ticket 06, ADR 0020): a
// fake WeChat platform serves its certificates and pushes a signed,
// encrypted TRANSACTION.SUCCESS notification; the real gateway verifies
// and decrypts it, the intake records the write-once evidence, advances
// the payment by CAS, and answers in the channel's dialect.

// ---- fake channel ----

type fakePlatform struct {
	mchID      string
	apiV3Key   string
	platKey    *rsa.PrivateKey
	platSerial string
	platPEM    string
	srv        *httptest.Server
}

func newFakePlatform(t *testing.T) *fakePlatform {
	t.Helper()
	var err error
	f := &fakePlatform{
		mchID:      fmt.Sprintf("1900000%d", time.Now().UnixNano()%100000000),
		apiV3Key:   "a1b2c3d4e5f6g7h8a1b2c3d4e5f6g7h8",
		platSerial: "PLAT-SERIAL-GOLD",
	}
	f.platKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("platform key: %v", err)
	}
	f.platPEM = selfSignedCertPEM(t, f.platKey)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/certificates", f.handleCertificates)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePlatform) handleCertificates(w http.ResponseWriter, r *http.Request) {
	nonce, ciphertext := f.encrypt("certificate", f.platPEM)
	raw, _ := json.Marshal(map[string]any{
		"data": []map[string]any{{
			"serial_no":      f.platSerial,
			"effective_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
			"expire_time":    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"encrypt_certificate": map[string]string{
				"algorithm": "AEAD_AES_256_GCM", "associated_data": "certificate",
				"nonce": nonce, "ciphertext": ciphertext,
			},
		}},
	})
	f.sign(w, raw)
	_, _ = w.Write(raw)
}

func (f *fakePlatform) sign(w http.ResponseWriter, body []byte) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := fmt.Sprintf("n%d", time.Now().UnixNano())
	message := ts + "\n" + nonce + "\n" + string(body) + "\n"
	digest := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.platKey, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	w.Header().Set("Wechatpay-Timestamp", ts)
	w.Header().Set("Wechatpay-Nonce", nonce)
	w.Header().Set("Wechatpay-Serial", f.platSerial)
	w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
	w.Header().Set("Content-Type", "application/json")
}

func (f *fakePlatform) encrypt(associatedData, plaintext string) (nonce, ciphertext string) {
	block, _ := aes.NewCipher([]byte(f.apiV3Key))
	aesgcm, _ := cipher.NewGCM(block)
	n := make([]byte, aesgcm.NonceSize())
	_, _ = rand.Read(n)
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	for i := range n {
		n[i] = alphabet[int(n[i])%len(alphabet)]
	}
	sealed := aesgcm.Seal(nil, n, []byte(plaintext), []byte(associatedData))
	return string(n), base64.StdEncoding.EncodeToString(sealed)
}

// paymentNotification builds one signed, encrypted TRANSACTION.SUCCESS
// delivery body plus its signature headers.
func (f *fakePlatform) paymentNotification(t *testing.T, outTradeNo, transactionID, payerID string) (http.Header, []byte) {
	t.Helper()
	plaintext, _ := json.Marshal(map[string]any{
		"out_trade_no": outTradeNo, "transaction_id": transactionID,
		"trade_state": "SUCCESS", "payer": map[string]string{"openid": payerID},
		"success_time": time.Now().UTC().Format(time.RFC3339),
	})
	nonce, ciphertext := f.encrypt("transaction", string(plaintext))
	body, _ := json.Marshal(map[string]any{
		"id": "EV-GOLD-1", "event_type": "TRANSACTION.SUCCESS",
		"resource_type": "encrypt-resource", "summary": "支付成功",
		"resource": map[string]string{
			"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM",
			"associated_data": "transaction", "nonce": nonce, "ciphertext": ciphertext,
		},
	})
	headers := http.Header{}
	headers.Set("Wechatpay-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	headers.Set("Wechatpay-Nonce", "gold-nonce")
	message := headers.Get("Wechatpay-Timestamp") + "\n" + "gold-nonce" + "\n" + string(body) + "\n"
	digest := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.platKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign notification: %v", err)
	}
	headers.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
	headers.Set("Wechatpay-Serial", f.platSerial)
	return headers, body
}

func goldPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("merchant key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal merchant key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func selfSignedCertPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: "gold-platform"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// ---- in-memory repo fakes over the real cipher ----

type goldMerchantRepo struct {
	m map[int64]*payentity.MerchantAccount
	c *crypt.Cipher
}

func (r *goldMerchantRepo) FindByID(ctx context.Context, id int64) (*payentity.MerchantAccount, error) {
	if m, ok := r.m[id]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, paydomain.ErrMerchantNotFound
}

func (r *goldMerchantRepo) List(context.Context, listq.Query) ([]*payentity.MerchantAccount, error) {
	return nil, nil
}

func (r *goldMerchantRepo) FindByIDAny(ctx context.Context, id int64) (*payentity.MerchantAccount, error) {
	return r.FindByID(ctx, id)
}

func (r *goldMerchantRepo) LoadConfig(ctx context.Context, m *payentity.MerchantAccount) error {
	values, err := r.c.Decrypt(r.m[m.ID].ConfigCipher)
	if err != nil {
		return err
	}
	m.Config = values
	return nil
}

func (r *goldMerchantRepo) Create(ctx context.Context, m *payentity.MerchantAccount) (*payentity.MerchantAccount, error) {
	token, err := r.c.Encrypt(m.Config)
	if err != nil {
		return nil, err
	}
	cp := *m
	cp.ConfigCipher = token
	cp.Config = nil
	r.m[m.ID] = &cp
	return &cp, nil
}

func (r *goldMerchantRepo) Update(ctx context.Context, req payrepo.UpdateMerchantRequest) (*payentity.MerchantAccount, error) {
	return req.Merchant, nil
}

func (r *goldMerchantRepo) UpdateStatus(ctx context.Context, req payrepo.UpdateMerchantStatusRequest) error {
	return nil
}

func (r *goldMerchantRepo) Delete(ctx context.Context, req payrepo.DeleteMerchantRequest) error {
	return nil
}

func (r *goldMerchantRepo) NextID() int64 { return 777 }

// goldApplicationRepo is unused by channel-notification intake. The
// payment service requires the port; intake never starts a Payment.
type goldApplicationRepo struct {
	payrepo.PayApplicationRepo
}

type goldPaymentRepo struct {
	p map[int64]*payentity.Payment
}

func (r *goldPaymentRepo) FindByID(ctx context.Context, id int64) (*payentity.Payment, error) {
	if p, ok := r.p[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, paydomain.ErrPaymentNotFound
}

func (r *goldPaymentRepo) FindByBizNo(ctx context.Context, req payrepo.FindByBizNoRequest) (*payentity.Payment, error) {
	return nil, paydomain.ErrPaymentNotFound
}

func (r *goldPaymentRepo) List(context.Context, listq.Query) ([]*payentity.Payment, error) {
	return nil, nil
}

func (r *goldPaymentRepo) ListPendingExpired(ctx context.Context, req payrepo.ListPendingExpiredRequest) ([]*payentity.Payment, error) {
	return nil, nil
}

func (r *goldPaymentRepo) Create(ctx context.Context, p *payentity.Payment) (*payentity.Payment, error) {
	cp := *p
	r.p[p.ID] = &cp
	return &cp, nil
}

func (r *goldPaymentRepo) SetChannelPayload(ctx context.Context, req payrepo.SetChannelPayloadRequest) error {
	return nil
}

func (r *goldPaymentRepo) SetFailReason(ctx context.Context, req payrepo.SetFailReasonRequest) error {
	return nil
}

func (r *goldPaymentRepo) MarkPaid(ctx context.Context, req payrepo.MarkPaidRequest) (bool, error) {
	p, ok := r.p[req.ID]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentPaid
	p.ChannelTradeNo = req.Capture.ChannelTradeNo
	p.PayerID = req.Capture.PayerID
	paidAt := req.Capture.PaidAt
	p.PaidAt = &paidAt
	return true, nil
}

func (r *goldPaymentRepo) BackfillPaidInfo(ctx context.Context, req payrepo.BackfillPaidInfoRequest) (bool, error) {
	return false, nil
}

func (r *goldPaymentRepo) MarkFailed(ctx context.Context, req payrepo.MarkPaymentFailedRequest) (bool, error) {
	return false, nil
}

func (r *goldPaymentRepo) MarkClosed(ctx context.Context, id int64) (bool, error) {
	p, ok := r.p[id]
	if !ok || p.Status != payentity.PaymentPending {
		return false, nil
	}
	p.Status = payentity.PaymentClosed
	return true, nil
}

func (r *goldPaymentRepo) MarkExpired(ctx context.Context, id int64) (bool, error) {
	return false, nil
}

func (r *goldPaymentRepo) NextID() int64 { return 800 }

type goldNotifyRow struct {
	notif     *payentity.ChannelNotification
	processed bool
}

type goldNotifyRepo struct {
	rows map[string]*goldNotifyRow // channel|notify_id → row
}

func (r *goldNotifyRepo) Insert(ctx context.Context, n *payentity.ChannelNotification) (bool, error) {
	key := fmt.Sprintf("%d|%s", n.Channel, n.NotifyID)
	if row, dup := r.rows[key]; dup {
		n.ID = row.notif.ID
		return false, nil
	}
	cp := *n
	r.rows[key] = &goldNotifyRow{notif: &cp}
	return true, nil
}

func (r *goldNotifyRepo) MarkProcessed(ctx context.Context, id int64) error { return nil }

func (r *goldNotifyRepo) NextID() int64 { return 900 }

// ---- fixtures ----

const goldMasterKey = "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY="

type goldFixture struct {
	platform *fakePlatform
	app      *pay.PayApplication
	payments *goldPaymentRepo
	notify   *goldNotifyRepo
	merchant payentity.MerchantAccount
}

func newGoldFixture(t *testing.T) *goldFixture {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, goldMasterKey)
	cipher := crypt.New()
	platform := newFakePlatform(t)

	merchant := payentity.MerchantAccount{
		ID:      42,
		Channel: payentity.ChannelWechat,
		Name:    "金路径商户",
		Status:  payentity.MerchantEnabled,
		Config: map[string]string{
			"mchid":                platform.mchID,
			"appid":                "wxGOLDTEST0001",
			"api_v3_key":           platform.apiV3Key,
			"merchant_serial":      "MCH-SERIAL-GOLD",
			"merchant_private_key": goldPrivateKeyPEM(t),
		},
	}
	merchantRepo := &goldMerchantRepo{m: map[int64]*payentity.MerchantAccount{}, c: cipher}
	if _, err := merchantRepo.Create(context.Background(), &merchant); err != nil {
		t.Fatalf("merchant fixture: %v", err)
	}
	payments := &goldPaymentRepo{p: map[int64]*payentity.Payment{}}
	pending := &payentity.Payment{
		ID:         20260911000001,
		MerchantID: 42,
		Channel:    payentity.ChannelWechat,
		BizNo:      "ORDER-GOLD",
		Subject:    "金路径",
		Amount:     100,
		Currency:   "CNY",
		PayMethod:  payentity.PayMethodNative,
		Status:     payentity.PaymentPending,
		ExpireTime: time.Now().Add(30 * time.Minute),
	}
	if _, err := payments.Create(context.Background(), pending); err != nil {
		t.Fatalf("payment fixture: %v", err)
	}
	notifyRepo := &goldNotifyRepo{rows: map[string]*goldNotifyRow{}}

	// Real domain services follow below, over the fakes and the real
	// wechat adapter factory.

	// A gateway registry whose factory injects the fake platform's
	// endpoint into every built gateway.
	base := platform.srv.Listener.Addr().String()
	registry := adapter.NewRegistry()
	registry.Register(&goldFactory{base: base})

	merchantSvc := payservice.NewMerchantService(merchantRepo, &goldCatalog{}, cipher, registry, slog.New(slog.DiscardHandler))
	paymentSvc := payservice.NewPaymentService(payments, goldApplicationRepo{}, merchantRepo, registry, slog.New(slog.DiscardHandler))
	refundSvc := payservice.NewRefundService(nil, payments, merchantRepo, registry, slog.New(slog.DiscardHandler))
	intake := payservice.NewIntake(merchantSvc, paymentSvc, refundSvc, notifyRepo, goldPublisher{}, slog.New(slog.DiscardHandler))

	app := pay.NewPayApplication(
		merchantSvc,
		nil,
		paymentSvc,
		refundSvc,
		intake,
		nil, nil,
		&conf.Pay{NotifyBaseUrl: "https://pay.example.com"},
		slog.New(slog.DiscardHandler),
	)
	return &goldFixture{
		platform: platform, app: app, payments: payments, notify: notifyRepo, merchant: merchant,
	}
}

type goldCatalog struct{}

func (goldCatalog) Schemas() []payentity.ChannelSchema { return nil }
func (goldCatalog) Schema(ch payentity.Channel) (payentity.ChannelSchema, error) {
	return payentity.ChannelSchema{}, paydomain.ErrChannelNotAdopted
}
func (goldCatalog) Adopted(payentity.Channel) bool { return true }
func (goldCatalog) ValidateConfig(req payservice.ChannelConfigRequest) (map[string]string, error) {
	return req.Values, nil
}
func (goldCatalog) MaskConfig(req payservice.ChannelConfigRequest) map[string]string {
	return req.Values
}

type goldFactory struct{ base string }

func (f *goldFactory) Schema() payentity.ChannelSchema { return wechat.ConfigSchema() }

func (f *goldFactory) NewGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	return wechat.NewFactory().NewGateway(cfg, append(opts, adapter.WithBaseURL(f.base))...)
}

// serve mounts the notify route alone and issues one request.
func (f *goldFixture) serve(t *testing.T) http.Handler {
	t.Helper()
	srv := khttp.NewServer()
	registerPayNotifyHTTP(srv, f.app, nil)
	return srv
}

func TestNotifyGoldPathRecordsAdvancesAndAcks(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	headers, body := f.platform.paymentNotification(t, "20260911000001", "wx-gold-txn-1", "openid-gold")

	req := httptest.NewRequest("POST", "/pay/notify/wechat/42", strings.NewReader(string(body)))
	req.Header = headers
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ack := rec.Body.String(); ack != `{"code":"SUCCESS","message":"成功"}` {
		t.Fatalf("ack = %q, want the wechat v3 dialect", ack)
	}
	// The payment advanced with the channel's evidence.
	p, err := f.payments.FindByID(context.Background(), 20260911000001)
	if err != nil {
		t.Fatalf("payment gone: %v", err)
	}
	if p.Status != payentity.PaymentPaid || p.ChannelTradeNo != "wx-gold-txn-1" || p.PayerID != "openid-gold" || p.PaidAt == nil {
		t.Fatalf("payment = %+v", p)
	}
	// The write-once evidence row exists, verified and processed.
	grow, ok := f.notify.rows["1|EV-GOLD-1"]
	if !ok || !grow.notif.Verified || grow.processed || grow.notif.TargetNo != "20260911000001" || grow.notif.RawBody == "" {
		t.Fatalf("evidence row = %+v, found = %v", grow, ok)
	}
}

func TestNotifyDuplicateDeliveryAcksWithoutReprocessing(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	headers, body := f.platform.paymentNotification(t, "20260911000001", "wx-gold-txn-1", "openid-gold")

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/pay/notify/wechat/42", strings.NewReader(string(body)))
		req.Header = headers
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != `{"code":"SUCCESS","message":"成功"}` {
			t.Fatalf("delivery %d: status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if got := len(f.notify.rows); got != 1 {
		t.Fatalf("evidence rows = %d, want exactly one", got)
	}
	p, _ := f.payments.FindByID(context.Background(), 20260911000001)
	if p.Status != payentity.PaymentPaid {
		t.Fatalf("status = %v", p.Status)
	}
}

func TestNotifyTamperedSignatureRecordedAndRejected(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	headers, body := f.platform.paymentNotification(t, "20260911000001", "wx-gold-txn-evil", "openid-evil")
	headers.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString([]byte("forged")))

	req := httptest.NewRequest("POST", "/pay/notify/wechat/42", strings.NewReader(string(body)))
	req.Header = headers
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	// The rejected delivery is evidence, not state.
	p, _ := f.payments.FindByID(context.Background(), 20260911000001)
	if p.Status != payentity.PaymentPending {
		t.Fatalf("rejected delivery changed state to %v", p.Status)
	}
	if got := len(f.notify.rows); got != 1 {
		t.Fatalf("evidence rows = %d, want the unverified one", got)
	}
	for _, row := range f.notify.rows {
		if row.notif.Verified {
			t.Fatal("forged delivery recorded as verified")
		}
		if row.processed {
			t.Fatal("forged delivery recorded as processed")
		}
	}
}

func TestNotifyUnknownMerchantIs404(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	headers, body := f.platform.paymentNotification(t, "20260911000001", "wx-x", "openid-x")

	req := httptest.NewRequest("POST", "/pay/notify/wechat/999999", strings.NewReader(string(body)))
	req.Header = headers
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestNotifyOversizedBodyIsRejected(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	// One byte past the cap is refused before any parsing happens.
	big := strings.Repeat("x", maxNotifyBodyBytes+1)
	req := httptest.NewRequest("POST", "/pay/notify/wechat/42", strings.NewReader(big))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	// Nothing was recorded — the delivery never reached the intake.
	if got := len(f.notify.rows); got != 0 {
		t.Fatalf("evidence rows = %d, want 0", got)
	}
	p, _ := f.payments.FindByID(context.Background(), 20260911000001)
	if p.Status != payentity.PaymentPending {
		t.Fatalf("oversized delivery changed state to %v", p.Status)
	}
}

func TestNotifyBodyAtCapIsProcessedNotSized(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	// A body exactly at the cap is read in full; it fails verification
	// (garbage), which records unverified evidence — proving the size
	// boundary itself did not reject it.
	body := strings.Repeat("x", maxNotifyBodyBytes)
	req := httptest.NewRequest("POST", "/pay/notify/wechat/42", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := len(f.notify.rows); got != 1 {
		t.Fatalf("evidence rows = %d, want the unverified row", got)
	}
}

func TestNotifyGarbagePathIs400(t *testing.T) {
	f := newGoldFixture(t)
	handler := f.serve(t)
	req := httptest.NewRequest("POST", "/pay/notify/nosuchchannel/42", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// goldPublisher stands in for the Payment Completed port. The event body
// is asserted on the domain intake; this path only needs the publish to
// succeed.
type goldPublisher struct{}

func (goldPublisher) Publish(context.Context, []byte) error { return nil }
