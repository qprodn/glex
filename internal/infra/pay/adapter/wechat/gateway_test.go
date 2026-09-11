package wechat

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
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"

	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
)

// fakeWechat is a contract-test fake of WeChat Pay APIv3: it serves the
// platform-certificate list encrypted with the merchant's APIv3 key, signs
// every success response with the platform key, and answers the prepay /
// query / close / refund endpoints the gateway speaks. Handler panics fail
// the running test through httptest's recovered-panic channel.
type fakeWechat struct {
	mchID      string
	apiV3Key   string
	mchKey     *rsa.PrivateKey
	platKey    *rsa.PrivateKey
	platSerial string
	platPEM    string

	codeURL    string
	states     map[string]payments.Transaction // out_trade_no → query result
	closeCodes map[string]int                  // out_trade_no → HTTP status (≠204 = ORDER_PAID)
	refunds    map[string]refunddomestic.Refund

	lastAuth      string
	lastMethod    string
	lastURI       string
	lastPrepayRaw []byte

	srv *httptest.Server
}

func newFakeWechat(t *testing.T) *fakeWechat {
	t.Helper()
	var err error
	f := &fakeWechat{
		mchID:      fmt.Sprintf("1900000%d", time.Now().UnixNano()%100000000),
		apiV3Key:   "a1b2c3d4e5f6g7h8a1b2c3d4e5f6g7h8",
		platSerial: "PLAT-SERIAL-0001",
		codeURL:    "weixin://wxpay/bizpayurl?pr=contracttest",
		states:     map[string]payments.Transaction{},
		closeCodes: map[string]int{},
		refunds:    map[string]refunddomestic.Refund{},
	}
	f.mchKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate merchant key: %v", err)
	}
	f.platKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate platform key: %v", err)
	}
	f.platPEM = selfSignedPEM(t, f.platKey)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/certificates", f.handleCertificates)
	mux.HandleFunc("POST /v3/pay/transactions/native", f.handlePrepay)
	mux.HandleFunc("GET /v3/pay/transactions/out-trade-no/", f.handleQuery)
	mux.HandleFunc("POST /v3/pay/transactions/out-trade-no/", f.handleClose)
	mux.HandleFunc("POST /v3/refund/domestic/refunds", f.handleRefundCreate)
	mux.HandleFunc("GET /v3/refund/domestic/refunds/", f.handleRefundQuery)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// gateway builds the real gateway over the fake server.
func (f *fakeWechat) gateway(t *testing.T) payservice.ChannelGateway {
	t.Helper()
	gw, err := newGateway(map[string]string{
		"mchid":                f.mchID,
		"appid":                "wxCONTRACTTEST01",
		"api_v3_key":           f.apiV3Key,
		"merchant_serial":      "MCH-SERIAL-0001",
		"merchant_private_key": privateKeyPEM(t, f.mchKey),
	}, adapter.WithBaseURL(f.srv.Listener.Addr().String()))
	if err != nil {
		t.Fatalf("newGateway: %v", err)
	}
	return gw
}

// merchantOK verifies the SDK's request signature against the merchant
// public key — proof the gateway signs with the configured credential.
func (f *fakeWechat) merchantOK(t *testing.T, body []byte) {
	t.Helper()
	if !strings.HasPrefix(f.lastAuth, "WECHATPAY2-SHA256-RSA2048 ") {
		t.Fatalf("authorization = %q", f.lastAuth)
	}
	parts := map[string]string{}
	for _, seg := range strings.Split(strings.TrimPrefix(f.lastAuth, "WECHATPAY2-SHA256-RSA2048 "), ",") {
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) == 2 {
			parts[kv[0]] = strings.Trim(kv[1], `"`)
		}
	}
	// Request signing message: METHOD\nURI\nts\nnonce\nbody\n.
	message := f.lastMethod + "\n" + f.lastURI + "\n" + parts["timestamp"] + "\n" + parts["nonce_str"] + "\n" + string(body) + "\n"
	sig, err := base64.StdEncoding.DecodeString(parts["signature"])
	if err != nil {
		t.Fatalf("authorization signature is not base64: %v", err)
	}
	digest := sha256.Sum256([]byte(message))
	if err := rsa.VerifyPKCS1v15(&f.mchKey.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("merchant request signature invalid: %v", err)
	}
}

// signBody signs a response body with the platform key.
func (f *fakeWechat) signBody(body []byte) http.Header {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := fmt.Sprintf("nonce-%d", time.Now().UnixNano())
	message := ts + "\n" + nonce + "\n" + string(body) + "\n"
	digest := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.platKey, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	h := http.Header{}
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", nonce)
	h.Set("Wechatpay-Serial", f.platSerial)
	h.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
	h.Set("Request-ID", "contract-test")
	return h
}

// encrypt seals plaintext with the APIv3 key (AES-256-GCM) — the channel's
// resource envelope.
func (f *fakeWechat) encrypt(associatedData, plaintext string) (nonce, ciphertext string) {
	block, err := aes.NewCipher([]byte(f.apiV3Key))
	if err != nil {
		panic(err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	// The nonce rides inside JSON, so it must be printable ASCII — the
	// channel's nonces are plain strings for the same reason.
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	n := make([]byte, aesgcm.NonceSize())
	if _, err := rand.Read(n); err != nil {
		panic(err)
	}
	for i := range n {
		n[i] = alphabet[int(n[i])%len(alphabet)]
	}
	sealed := aesgcm.Seal(nil, n, []byte(plaintext), []byte(associatedData))
	// The channel's nonce is a 12-character plain string (not base64).
	return string(n), base64.StdEncoding.EncodeToString(sealed)
}

func (f *fakeWechat) writeSigned(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	for k, vs := range f.signBody(raw) {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// ---- handlers ----

func (f *fakeWechat) handleCertificates(w http.ResponseWriter, r *http.Request) {
	nonce, ciphertext := f.encrypt("certificate", f.platPEM)
	f.writeSigned(w, http.StatusOK, map[string]any{
		"data": []map[string]any{{
			"serial_no":      f.platSerial,
			"effective_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
			"expire_time":    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"encrypt_certificate": map[string]string{
				"algorithm":       "AEAD_AES_256_GCM",
				"associated_data": "certificate",
				"nonce":           nonce,
				"ciphertext":      ciphertext,
			},
		}},
	})
}

func (f *fakeWechat) handlePrepay(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.lastAuth = r.Header.Get("Authorization")
	f.lastMethod = r.Method
	f.lastURI = r.URL.RequestURI()
	f.lastPrepayRaw = raw
	f.writeSigned(w, http.StatusOK, map[string]string{"code_url": f.codeURL})
}

func (f *fakeWechat) handleQuery(w http.ResponseWriter, r *http.Request) {
	no := pathSegment(r.URL.Path, "out-trade-no")
	tx, ok := f.states[no]
	if !ok {
		f.writeError(w, http.StatusNotFound, "ORDER_NOT_EXIST", "order not exists")
		return
	}
	f.writeSigned(w, http.StatusOK, tx)
}

func (f *fakeWechat) handleClose(w http.ResponseWriter, r *http.Request) {
	no := pathSegment(r.URL.Path, "out-trade-no")

	if code, ok := f.closeCodes[no]; ok && code != http.StatusNoContent {
		f.writeError(w, code, "ORDER_PAID", "order paid")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeWechat) handleRefundCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OutRefundNo *string `json:"out_refund_no"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.writeError(w, http.StatusBadRequest, "PARAM_ERROR", "bad body")
		return
	}
	rf, ok := f.refunds[deref(req.OutRefundNo)]
	if !ok {
		f.writeError(w, http.StatusNotFound, "RESOURCE_NOT_EXISTS", "refund not exists")
		return
	}
	f.writeSigned(w, http.StatusOK, rf)
}

func (f *fakeWechat) handleRefundQuery(w http.ResponseWriter, r *http.Request) {
	// The channel's query path is /v3/refund/domestic/refunds/{out_refund_no}.
	no := pathSegment(r.URL.Path, "refunds")
	rf, ok := f.refunds[no]
	if !ok {
		f.writeError(w, http.StatusNotFound, "RESOURCE_NOT_EXISTS", "refund not exists")
		return
	}
	f.writeSigned(w, http.StatusOK, rf)
}

// ---- notification builders (the channel pushing to US) ----

// notifyHeaders signs a notification body with the platform key — exactly
// what the real channel sends to our notify endpoint.
func (f *fakeWechat) notifyHeaders(body []byte) http.Header {
	return f.signBody(body)
}

// paymentNotification builds a TRANSACTION.SUCCESS delivery for one
// transaction.
func (f *fakeWechat) paymentNotification(t *testing.T, tx payments.Transaction) (http.Header, []byte) {
	t.Helper()
	plaintext, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}
	nonce, ciphertext := f.encrypt("transaction", string(plaintext))
	body, err := json.Marshal(map[string]any{
		"id":            "EV-TEST-0001",
		"create_time":   time.Now().Format(time.RFC3339),
		"event_type":    "TRANSACTION.SUCCESS",
		"resource_type": "encrypt-resource",
		"summary":       "支付成功",
		"resource": map[string]string{
			"original_type":   "transaction",
			"algorithm":       "AEAD_AES_256_GCM",
			"associated_data": "transaction",
			"nonce":           nonce,
			"ciphertext":      ciphertext,
		},
	})
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	return f.notifyHeaders(body), body
}

// refundNotification builds a REFUND.SUCCESS delivery for one refund.
func (f *fakeWechat) refundNotification(t *testing.T, rf refunddomestic.Refund) (http.Header, []byte) {
	t.Helper()
	plaintext, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal refund: %v", err)
	}
	nonce, ciphertext := f.encrypt("refund", string(plaintext))
	body, err := json.Marshal(map[string]any{
		"id":          "EV-TEST-0002",
		"create_time": time.Now().Format(time.RFC3339),
		"event_type":  "REFUND.SUCCESS",
		"resource": map[string]string{
			"original_type":   "refund",
			"algorithm":       "AEAD_AES_256_GCM",
			"associated_data": "refund",
			"nonce":           nonce,
			"ciphertext":      ciphertext,
		},
	})
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	return f.notifyHeaders(body), body
}

func refundFixture(outRefundNo, outTradeNo, refundID, txnID, status string) refunddomestic.Refund {
	ch := refunddomestic.Channel("ORIGINAL")
	st := refunddomestic.Status(status)
	now := time.Now()
	return refunddomestic.Refund{
		OutRefundNo:         &outRefundNo,
		OutTradeNo:          &outTradeNo,
		RefundId:            &refundID,
		TransactionId:       &txnID,
		Channel:             &ch,
		UserReceivedAccount: strPtr("支付用户零钱"),
		CreateTime:          &now,
		Status:              &st,
		Amount: &refunddomestic.Amount{
			Currency:         strPtr("CNY"),
			Total:            i64Ptr(100),
			Refund:           i64Ptr(40),
			PayerTotal:       i64Ptr(100),
			PayerRefund:      i64Ptr(40),
			SettlementTotal:  i64Ptr(100),
			SettlementRefund: i64Ptr(40),
			DiscountRefund:   i64Ptr(0),
		},
	}
}

// ---- small helpers ----

func (f *fakeWechat) writeError(w http.ResponseWriter, status int, code, message string) {
	body := fmt.Sprintf(`{"code":%q,"message":%q}`, code, message)
	for k, vs := range f.signBody([]byte(body)) {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func pathSegment(path, marker string) string {
	idx := strings.Index(path, marker+"/")
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(marker)+1:]
	if cut := strings.IndexAny(rest, "/?"); cut >= 0 {
		rest = rest[:cut]
	}
	return rest
}

func selfSignedPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake-wechat-platform"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func privateKeyPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// ---- tests ----

func TestPrepayReturnsCodeURLWithSignedRequest(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	out, err := gw.Prepay(context.Background(), payservice.PrepayRequest{
		PaymentNo: "20260911000001",
		Amount:    100,
		Currency:  "CNY",
		Subject:   "测试商品",
		ExpireAt:  time.Now().Add(30 * time.Minute),
		Method:    payentity.PayMethodNative,
		NotifyURL: "https://glex.example.com/pay/notify/wechat/1",
	})
	if err != nil {
		t.Fatalf("prepay: %v", err)
	}
	if out.ChannelPayload != f.codeURL {
		t.Fatalf("code_url = %q, want %q", out.ChannelPayload, f.codeURL)
	}
	// The request the SDK sent was signed with the merchant credential and
	// carries the fen amount as-is (identity unit conversion, ADR 0019).
	f.merchantOK(t, f.lastPrepayRaw)
	if !strings.Contains(string(f.lastPrepayRaw), `"total":100`) {
		t.Fatalf("prepay body missing fen amount: %s", f.lastPrepayRaw)
	}
	if !strings.Contains(string(f.lastPrepayRaw), `"out_trade_no":"20260911000001"`) {
		t.Fatalf("prepay body missing out_trade_no: %s", f.lastPrepayRaw)
	}
}

func TestQueryMapsTradeStates(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	paid := time.Now()
	f.states["PAID-1"] = payments.Transaction{
		OutTradeNo:    strPtr("PAID-1"),
		TradeState:    strPtr("SUCCESS"),
		TransactionId: strPtr("wx-txn-1"),
		SuccessTime:   strPtr(paid.UTC().Format(time.RFC3339)),
		Payer:         &payments.TransactionPayer{Openid: strPtr("openid-1")},
		Amount:        &payments.TransactionAmount{Total: i64Ptr(100), Currency: strPtr("CNY")},
	}
	state, err := gw.Query(context.Background(), nil, "PAID-1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if state.State != payentity.PaymentPaid || state.ChannelTradeNo != "wx-txn-1" ||
		state.PayerID != "openid-1" || state.PaidAt.IsZero() {
		t.Fatalf("state = %+v", state)
	}
	// The channel-asserted total rides along for the settle-path amount
	// verification (ADR 0019).
	if state.Amount != 100 {
		t.Fatalf("asserted amount = %d, want 100", state.Amount)
	}
	f.states["CLOSED-1"] = payments.Transaction{OutTradeNo: strPtr("CLOSED-1"), TradeState: strPtr("CLOSED")}
	state, err = gw.Query(context.Background(), nil, "CLOSED-1")
	if err != nil || state.State != payentity.PaymentClosed {
		t.Fatalf("closed state = %+v, err = %v", state, err)
	}
	f.states["ERR-1"] = payments.Transaction{OutTradeNo: strPtr("ERR-1"), TradeState: strPtr("PAYERROR")}
	state, err = gw.Query(context.Background(), nil, "ERR-1")
	if err != nil || state.State != payentity.PaymentFailed {
		t.Fatalf("failed state = %+v, err = %v", state, err)
	}
	f.states["WAIT-1"] = payments.Transaction{OutTradeNo: strPtr("WAIT-1"), TradeState: strPtr("NOTPAY")}
	state, err = gw.Query(context.Background(), nil, "WAIT-1")
	if err != nil || state.State != payentity.PaymentPending {
		t.Fatalf("pending state = %+v, err = %v", state, err)
	}
}

func TestProbeSucceedsOnOrderNotExist(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	// No states registered — every query answers ORDER_NOT_EXIST with a
	// verifiable response; that is exactly the probe's success condition.
	if err := gw.Probe(context.Background(), nil); err != nil {
		t.Fatalf("probe: %v", err)
	}
}

func TestCloseAlreadyPaidSignalsFlip(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	f.closeCodes["GONE-1"] = http.StatusForbidden
	err := gw.Close(context.Background(), nil, "GONE-1")
	if !errorsIs(err, payservice.ErrChannelAlreadyPaid) {
		t.Fatalf("err = %v, want ErrChannelAlreadyPaid", err)
	}
}

func TestRefundAndQueryRefund(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	refundedAt := time.Now().Add(-time.Minute)
	f.refunds["RF-1"] = refundFixture("RF-1", "PAY-1", "wx-refund-1", "wx-txn-1", "PROCESSING")
	out, err := gw.Refund(context.Background(), payservice.RefundRequest{
		RefundNo:  "RF-1",
		PaymentNo: "PAY-1",
		Total:     100,
		Amount:    40,
		Reason:    "部分退款",
		NotifyURL: "https://glex.example.com/pay/notify/wechat/1",
	})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if out.State != payentity.RefundPending {
		t.Fatalf("processing refund state = %v, want Pending", out.State)
	}
	state, err := gw.QueryRefund(context.Background(), nil, "RF-1")
	if err != nil {
		t.Fatalf("query refund: %v", err)
	}
	if state.State != payentity.RefundPending {
		t.Fatalf("query refund state = %v", state.State)
	}
	// The channel settled it meanwhile.
	settled := refundFixture("RF-1", "PAY-1", "wx-refund-1", "wx-txn-1", "SUCCESS")
	settled.SuccessTime = &refundedAt
	f.refunds["RF-1"] = settled
	state, err = gw.QueryRefund(context.Background(), nil, "RF-1")
	if err != nil {
		t.Fatalf("query refund: %v", err)
	}
	if state.State != payentity.RefundSuccess || state.ChannelRefundNo != "wx-refund-1" || state.RefundedAt.IsZero() {
		t.Fatalf("settled state = %+v", state)
	}
	if state.RefundAmount != 40 {
		t.Fatalf("asserted refund amount = %d, want 40", state.RefundAmount)
	}
	// ABNORMAL maps to failed with a reason.
	f.refunds["RF-2"] = refundFixture("RF-2", "PAY-1", "wx-refund-2", "wx-txn-2", "ABNORMAL")
	out, err = gw.Refund(context.Background(), payservice.RefundRequest{RefundNo: "RF-2", PaymentNo: "PAY-1", Total: 100, Amount: 1})
	if err != nil {
		t.Fatalf("refund RF-2: %v", err)
	}
	if out.State != payentity.RefundFailed || out.FailReason == "" {
		t.Fatalf("abnormal refund state = %+v", out)
	}
}

func TestParseNotificationPaymentSuccess(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	paid := time.Now().Truncate(time.Second)
	headers, body := f.paymentNotification(t, payments.Transaction{
		OutTradeNo:    strPtr("PAY-9"),
		TransactionId: strPtr("wx-txn-9"),
		TradeState:    strPtr("SUCCESS"),
		SuccessTime:   strPtr(paid.UTC().Format(time.RFC3339)),
		Payer:         &payments.TransactionPayer{Openid: strPtr("openid-9")},
		Amount:        &payments.TransactionAmount{Total: i64Ptr(100), Currency: strPtr("CNY")},
	})
	out, err := gw.ParseNotification(context.Background(), nil, headers, body)
	if err != nil {
		t.Fatalf("parse notification: %v", err)
	}
	if !out.Verified || out.Kind != payentity.NotifyKindPayment || out.TargetNo != "PAY-9" ||
		out.ChannelTradeNo != "wx-txn-9" || out.PayerID != "openid-9" || out.NotifyID != "EV-TEST-0001" {
		t.Fatalf("notification = %+v", out)
	}
	if out.PaidAt.Unix() != paid.Unix() {
		t.Fatalf("paid_at = %v, want ~%v", out.PaidAt, paid)
	}
	if out.Amount != 100 {
		t.Fatalf("asserted amount = %d, want 100", out.Amount)
	}
}

func TestParseNotificationRefundSuccess(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	refundedAt := time.Now().Truncate(time.Second)
	notifRefund := refundFixture("RF-9", "PAY-9", "wx-refund-9", "wx-txn-9", "SUCCESS")
	notifRefund.SuccessTime = &refundedAt
	headers, body := f.refundNotification(t, notifRefund)
	out, err := gw.ParseNotification(context.Background(), nil, headers, body)
	if err != nil {
		t.Fatalf("parse notification: %v", err)
	}
	if !out.Verified || out.Kind != payentity.NotifyKindRefund || out.TargetNo != "RF-9" ||
		out.RefundState != payentity.RefundSuccess || out.NotifyID != "EV-TEST-0002" {
		t.Fatalf("notification = %+v", out)
	}
	if out.RefundAmount != 40 {
		t.Fatalf("asserted refund amount = %d, want 40", out.RefundAmount)
	}
}

func TestParseNotificationRejectsBadSignature(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	headers, body := f.paymentNotification(t, payments.Transaction{
		OutTradeNo: strPtr("PAY-X"), TradeState: strPtr("SUCCESS"),
	})
	// Tamper with the signature — the delivery must be rejected.
	headers.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString([]byte("forged")))
	if _, err := gw.ParseNotification(context.Background(), nil, headers, body); err == nil {
		t.Fatal("forged notification accepted")
	}
	// Tamper with the body against a valid signature — same rejection.
	headers, body = f.paymentNotification(t, payments.Transaction{
		OutTradeNo: strPtr("PAY-Y"), TradeState: strPtr("SUCCESS"),
	})
	body = append(body, ' ')
	if _, err := gw.ParseNotification(context.Background(), nil, headers, body); err == nil {
		t.Fatal("body-substituted notification accepted")
	}
}

func TestParseNotificationRejectsUnknownEventType(t *testing.T) {
	f := newFakeWechat(t)
	gw := f.gateway(t)
	body := []byte(`{"id":"EV-X","event_type":"MCHROOSTER.SOMETHING","resource":{}}`)
	if _, err := gw.ParseNotification(context.Background(), nil, f.notifyHeaders(body), body); err == nil {
		t.Fatal("unknown event type accepted")
	}
}

// ---- tiny aliases ----

func strPtr(s string) *string { return &s }

func i64Ptr(v int64) *int64 { return &v }

func refundStatusPtr(s string) *refunddomestic.Status {
	st := refunddomestic.Status(s)
	return &st
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type is interface{ Is(error) bool }
		if i, ok := err.(is); ok && i.Is(target) {
			return true
		}
		type unwrap interface{ Unwrap() error }
		u, ok := err.(unwrap)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
