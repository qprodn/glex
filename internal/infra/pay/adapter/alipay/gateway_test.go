package alipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"

	"github.com/smartwalle/alipay/v3"
)

// fakeAlipay is a contract-test fake of Alipay's openapi gateway: one
// endpoint, form-encoded requests routed by `method`, responses signed
// with the Alipay-side key over the raw response JSON.
type fakeAlipay struct {
	appID     string
	alipayKey *rsa.PrivateKey
	alipayPEM string // what the config's alipay_public_key holds

	appKey *rsa.PrivateKey // the gateway config's application private key

	trades  map[string]*tradeState
	refunds map[string]*refundState

	// captured request evidence
	lastBiz       map[string]string
	lastSignValid bool
	gatewayHost   string

	srv *httptest.Server
}

type tradeState struct {
	status  alipay.TradeStatus
	tradeNo string
	buyer   string
	payDate string
}

type refundState struct {
	refunded     bool
	payTime      string
	notExist     bool
	systemError  bool // the channel answers ACQ.SYSTEM_ERROR — an inconclusive outcome
}

func newFakeAlipay(t *testing.T) *fakeAlipay {
	t.Helper()
	var err error
	f := &fakeAlipay{
		appID:   "2021000100000001",
		trades:  map[string]*tradeState{},
		refunds: map[string]*refundState{},
	}
	f.appKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	f.alipayKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate alipay key: %v", err)
	}
	f.alipayPEM = publicKeyPEM(t, &f.alipayKey.PublicKey)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /gateway.do", f.handleGateway)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.gatewayHost = f.srv.URL + "/gateway.do"
	return f
}

// gateway builds the real gateway over the fake server (key mode).
func (f *fakeAlipay) gateway(t *testing.T) payservice.ChannelGateway {
	t.Helper()
	gw, err := newGateway(map[string]string{
		"app_id":            f.appID,
		"mode":              "key",
		"app_private_key":   privateKeyPEM(t, f.appKey),
		"alipay_public_key": f.alipayPEM,
	}, adapter.WithBaseURL(f.gatewayHost))
	if err != nil {
		t.Fatalf("newGateway: %v", err)
	}
	return gw
}

// appOK verifies the SDK's request signature against the application
// public key, recording the outcome — proof the gateway signs with the
// configured credential. The assertion happens in the test body.
func (f *fakeAlipay) appOK(values url.Values) {
	sig, err := base64.StdEncoding.DecodeString(values.Get("sign"))
	if err != nil {
		f.lastSignValid = false
		return
	}
	message := canonicalValues(values, "sign")
	digest := sha256.Sum256([]byte(message))
	f.lastSignValid = rsa.VerifyPKCS1v15(&f.appKey.PublicKey, crypto.SHA256, digest[:], sig) == nil
}

func (f *fakeAlipay) handleGateway(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	values := r.PostForm
	f.lastBiz = map[string]string{}
	for k := range values {
		f.lastBiz[k] = values.Get(k)
	}
	method := values.Get("method")
	var biz map[string]any
	var bizField = strings.Replace(method, ".", "_", -1) + "_response"
	switch method {
	case "alipay.trade.precreate":
		f.appOK(values)
		biz = f.handleBiz(method, values.Get("biz_content"))
	case "alipay.trade.query", "alipay.trade.close", "alipay.trade.refund", "alipay.trade.fastpay.refund.query":
		biz = f.handleBiz(method, values.Get("biz_content"))
	default:
		biz = map[string]any{"code": "40002", "msg": "Invalid Arguments", "sub_code": "ACQ.PARAM_ERROR", "sub_msg": "unknown method"}
	}
	f.writeSigned(w, bizField, biz)
}

func (f *fakeAlipay) handleBiz(method, bizContent string) map[string]any {
	var biz map[string]any
	_ = json.Unmarshal([]byte(bizContent), &biz)
	outTradeNo, _ := biz["out_trade_no"].(string)
	outRequestNo, _ := biz["out_request_no"].(string)
	switch method {
	case "alipay.trade.precreate":
		return map[string]any{
			"code": "10000", "msg": "Success",
			"out_trade_no": outTradeNo,
			"qr_code":      "https://qr.alipay.com/contracttest",
		}
	case "alipay.trade.query":
		st, ok := f.trades[outTradeNo]
		if !ok {
			return bizError("ACQ.TRADE_NOT_EXIST", "交易不存在")
		}
		return map[string]any{
			"code": "10000", "msg": "Success",
			"trade_no": st.tradeNo, "out_trade_no": outTradeNo,
			"trade_status": string(st.status), "buyer_logon_id": st.buyer,
			"send_pay_date": st.payDate,
			"total_amount":  "1.00",
		}
	case "alipay.trade.close":
		st, ok := f.trades[outTradeNo]
		if !ok {
			return bizError("ACQ.TRADE_NOT_EXIST", "交易不存在")
		}
		if st.status == alipay.TradeStatusSuccess || st.status == alipay.TradeStatusFinished {
			return bizError("ACQ.TRADE_STATUS_ERROR", "交易状态不允许关闭")
		}
		st.status = alipay.TradeStatusClosed
		return map[string]any{"code": "10000", "msg": "Success", "trade_no": st.tradeNo, "out_trade_no": outTradeNo}
	case "alipay.trade.refund":
		rf, ok := f.refunds[outRequestNo]
		if !ok || rf.notExist {
			return bizError("ACQ.REFUND_AMT_NOT_EQUAL", "退款金额不一致")
		}
		if rf.systemError {
			return bizError("ACQ.SYSTEM_ERROR", "系统异常")
		}
		fundChange := "Y"
		if rf.refunded {
			fundChange = "N" // repeat of an already-processed refund
		}
		rf.refunded = true
		return map[string]any{"code": "10000", "msg": "Success", "fund_change": fundChange, "out_request_no": outRequestNo}
	case "alipay.trade.fastpay.refund.query":
		// The real gateway rejects a query carrying no out_trade_no with
		// a parameter error — the contract under test must too.
		if _, ok := biz["out_trade_no"].(string); !ok || biz["out_trade_no"] == "" {
			return bizError("ACQ.INVALID_PARAMETER", "参数错误")
		}
		if rf, ok := f.refunds[outRequestNo]; ok && rf.systemError {
			return bizError("ACQ.SYSTEM_ERROR", "系统异常")
		}
		rf, ok := f.refunds[outRequestNo]
		if !ok || rf.notExist {
			return bizError("ACQ.TRADE_NOT_EXIST", "交易不存在")
		}
		if !rf.refunded {
			return map[string]any{"code": "10000", "msg": "Success", "out_request_no": outRequestNo}
		}
		return map[string]any{
			"code": "10000", "msg": "Success",
			"out_request_no": outRequestNo,
			"refund_status":  "REFUND_SUCCESS",
			"gmt_refund_pay": rf.payTime,
			"refund_amount":  "0.40",
		}
	}
	return bizError("ACQ.PARAM_ERROR", "unreachable")
}

func bizError(subCode, subMsg string) map[string]any {
	return map[string]any{"code": "40004", "msg": "Business Failed", "sub_code": subCode, "sub_msg": subMsg}
}

// writeSigned renders one gateway response: the raw biz JSON, signed with
// the Alipay-side key — exactly what the real channel returns.
func (f *fakeAlipay) writeSigned(w http.ResponseWriter, bizField string, biz map[string]any) {
	raw, err := json.Marshal(biz)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.alipayKey, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	body := fmt.Sprintf(`{"%s":%s,"sign":"%s"}`, bizField, raw, base64.StdEncoding.EncodeToString(sig))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

// notification builds a signed trade_status_sync delivery (the channel
// pushing to US) as a form body.
func (f *fakeAlipay) notification(t *testing.T, fields map[string]string) (http.Header, []byte) {
	t.Helper()
	values := url.Values{}
	values.Set("app_id", f.appID)
	values.Set("notify_type", "trade_status_sync")
	values.Set("sign_type", "RSA2")
	values.Set("charset", "utf-8")
	values.Set("version", "1.0")
	values.Set("notify_id", "NOTIFY-0001")
	for k, v := range fields {
		values.Set(k, v)
	}
	message := canonicalValues(values, "sign", "sign_type", "alipay_cert_sn")
	digest := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.alipayKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign notification: %v", err)
	}
	values.Set("sign", base64.StdEncoding.EncodeToString(sig))
	return nil, []byte(values.Encode())
}

// canonicalValues replicates nsign's DefaultEncoder: sorted k=v pairs
// (values trimmed), ignoring the named keys, joined with &.
func canonicalValues(values url.Values, ignore ...string) string {
	skips := map[string]bool{}
	for _, k := range ignore {
		skips[k] = true
	}
	var pairs []string
	for k, vs := range values {
		if skips[k] {
			continue
		}
		for _, v := range vs {
			// nsign skips empty values entirely.
			if nv := strings.TrimSpace(v); nv != "" {
				pairs = append(pairs, k+"="+nv)
			}
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

func publicKeyPEM(t *testing.T, key *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
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

func TestPrecreateReturnsQRCodeAndSignedRequest(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	out, err := gw.Prepay(context.Background(), payservice.PrepayRequest{
		PaymentNo: "20260911000001",
		Amount:    100, // 1.00 yuan
		Currency:  "CNY",
		Subject:   "测试商品",
		ExpireAt:  time.Now().Add(30 * time.Minute),
		Method:    payentity.PayMethodNative,
		NotifyURL: "https://glex.example.com/pay/notify/alipay/1",
	})
	if err != nil {
		t.Fatalf("prepay: %v", err)
	}
	if out.ChannelPayload == "" {
		t.Fatal("prepay returned no QR payload")
	}
	// The request was signed with the application credential and the
	// amount converted fen → yuan string (ADR 0019's one conversion).
	f.appOK(formOf(f.lastBiz))
	if !f.lastSignValid {
		t.Fatal("application request signature invalid")
	}
	if f.lastBiz["biz_content"] == "" || !strings.Contains(f.lastBiz["biz_content"], `"total_amount":"1.00"`) {
		t.Fatalf("precreate biz = %s", f.lastBiz["biz_content"])
	}
	if !strings.Contains(f.lastBiz["biz_content"], `"out_trade_no":"20260911000001"`) {
		t.Fatalf("precreate biz = %s", f.lastBiz["biz_content"])
	}
}

func TestQueryMapsTradeStatuses(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	f.trades["PAID-1"] = &tradeState{
		status:  alipay.TradeStatusSuccess,
		tradeNo: "ali-txn-1",
		buyer:   "ali***@sandbox.com",
		payDate: "2026-09-11 10:00:00",
	}
	state, err := gw.Query(context.Background(), nil, "PAID-1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if state.State != payentity.PaymentPaid || state.ChannelTradeNo != "ali-txn-1" || state.PayerID == "" || state.PaidAt.IsZero() {
		t.Fatalf("state = %+v", state)
	}
	// The channel-asserted total rides along for the settle-path amount
	// verification (ADR 0019).
	if state.Amount != 100 {
		t.Fatalf("asserted amount = %d, want 100", state.Amount)
	}
	f.trades["CLOSED-1"] = &tradeState{status: alipay.TradeStatusClosed, tradeNo: "ali-txn-2"}
	state, err = gw.Query(context.Background(), nil, "CLOSED-1")
	if err != nil || state.State != payentity.PaymentClosed {
		t.Fatalf("closed state = %+v, err = %v", state, err)
	}
	f.trades["WAIT-1"] = &tradeState{status: alipay.TradeStatusWaitBuyerPay, tradeNo: "ali-txn-3"}
	state, err = gw.Query(context.Background(), nil, "WAIT-1")
	if err != nil || state.State != payentity.PaymentPending {
		t.Fatalf("pending state = %+v, err = %v", state, err)
	}
	// A trade the channel has never heard of is pending, not an error —
	// precreate and query race legitimately.
	state, err = gw.Query(context.Background(), nil, "GHOST-1")
	if err != nil || state.State != payentity.PaymentPending {
		t.Fatalf("ghost state = %+v, err = %v", state, err)
	}
}

func TestProbeSucceedsOnTradeNotExist(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	if err := gw.Probe(context.Background(), nil); err != nil {
		t.Fatalf("probe: %v", err)
	}
}

func TestCloseAlreadyPaidSignalsFlip(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	f.trades["PAID-X"] = &tradeState{status: alipay.TradeStatusSuccess, tradeNo: "ali-txn-x"}
	err := gw.Close(context.Background(), nil, "PAID-X")
	if !errorsIs(err, payservice.ErrChannelAlreadyPaid) {
		t.Fatalf("err = %v, want ErrChannelAlreadyPaid", err)
	}
	// A closable trade closes silently.
	f.trades["WAIT-X"] = &tradeState{status: alipay.TradeStatusWaitBuyerPay, tradeNo: "ali-txn-y"}
	if err := gw.Close(context.Background(), nil, "WAIT-X"); err != nil {
		t.Fatalf("close pending: %v", err)
	}
}

func TestRefundSynchronousSettlement(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	f.refunds["RF-1"] = &refundState{}
	out, err := gw.Refund(context.Background(), payservice.RefundRequest{
		RefundNo:  "RF-1",
		PaymentNo: "PAY-1",
		Total:     100,
		Amount:    40, // 0.40 yuan
		Reason:    "部分退款",
	})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if out.State != payentity.RefundSuccess || out.RefundedAt.IsZero() {
		t.Fatalf("refund state = %+v", out)
	}
	if out.RefundAmount != 40 {
		t.Fatalf("refund amount = %d, want 40", out.RefundAmount)
	}
	// The amount crossed as a yuan string (fen → yuan is the adapter's one
	// conversion, ADR 0019).
	if !strings.Contains(f.lastBiz["biz_content"], `"refund_amount":"0.40"`) {
		t.Fatalf("refund biz = %s", f.lastBiz["biz_content"])
	}
	// A repeated identical refund still settles (fund_change N).
	out, err = gw.Refund(context.Background(), payservice.RefundRequest{
		RefundNo: "RF-1", PaymentNo: "PAY-1", Total: 100, Amount: 40,
	})
	if err != nil || out.State != payentity.RefundSuccess {
		t.Fatalf("repeat refund = %+v, err = %v", out, err)
	}
	// A channel-side hard refusal returns a Failed state with the reason.
	f.refunds["RF-2"] = &refundState{notExist: true}
	out, err = gw.Refund(context.Background(), payservice.RefundRequest{
		RefundNo: "RF-2", PaymentNo: "PAY-1", Total: 100, Amount: 1,
	})
	if err != nil {
		t.Fatalf("refund RF-2: %v", err)
	}
	if out.State != payentity.RefundFailed || out.FailReason == "" {
		t.Fatalf("failed refund = %+v", out)
	}
	// An inconclusive outcome (system error) is an error — the row stays
	// Pending for an idempotent same-number retry, never Failed.
	f.refunds["RF-3"] = &refundState{systemError: true}
	if _, err := gw.Refund(context.Background(), payservice.RefundRequest{
		RefundNo: "RF-3", PaymentNo: "PAY-1", Total: 100, Amount: 1,
	}); err == nil {
		t.Fatal("inconclusive refund outcome did not error")
	}
}

func TestQueryRefund(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	f.refunds["RF-9"] = &refundState{refunded: true, payTime: "2026-09-11 10:30:00"}
	state, err := gw.QueryRefund(context.Background(), nil, "RF-9", "PAY-9")
	if err != nil {
		t.Fatalf("query refund: %v", err)
	}
	if state.State != payentity.RefundSuccess || state.RefundedAt.IsZero() {
		t.Fatalf("refund state = %+v", state)
	}
	if state.RefundAmount != 40 {
		t.Fatalf("refund amount = %d, want 40", state.RefundAmount)
	}
	// Unknown refunds are pending, not errors.
	f.refunds["RF-GHOST"] = &refundState{notExist: true}
	state, err = gw.QueryRefund(context.Background(), nil, "RF-GHOST", "PAY-9")
	if err != nil || state.State != payentity.RefundPending {
		t.Fatalf("ghost refund = %+v, err = %v", state, err)
	}
	// A query failure is an error, never a terminal Failed state — a
	// query cannot assert an outcome it did not observe.
	f.refunds["RF-ERR"] = &refundState{notExist: true, systemError: true}
	if _, err := gw.QueryRefund(context.Background(), nil, "RF-ERR", "PAY-9"); err == nil {
		t.Fatal("query failure settled the refund as Failed")
	}
	// Dropping the payment number is a parameter error at the real gateway.
	if _, err := gw.QueryRefund(context.Background(), nil, "RF-9", ""); err == nil {
		t.Fatal("query without out_trade_no accepted")
	}
}

func TestParseNotificationPaymentSuccess(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	headers, body := f.notification(t, map[string]string{
		"trade_no":       "ali-txn-9",
		"out_trade_no":   "PAY-9",
		"trade_status":   "TRADE_SUCCESS",
		"buyer_logon_id": "ali***@sandbox.com",
		"gmt_payment":    "2026-09-11 10:10:00",
		"total_amount":   "1.00",
	})
	out, err := gw.ParseNotification(context.Background(), nil, headers, body)
	if err != nil {
		t.Fatalf("parse notification: %v", err)
	}
	if !out.Verified || out.Kind != payentity.NotifyKindPayment || out.TargetNo != "PAY-9" ||
		out.ChannelTradeNo != "ali-txn-9" || out.PayerID == "" || out.NotifyID != "NOTIFY-0001" {
		t.Fatalf("notification = %+v", out)
	}
	if out.PaidAt.IsZero() {
		t.Fatal("paid_at missing")
	}
	if out.Amount != 100 {
		t.Fatalf("asserted amount = %d, want 100", out.Amount)
	}
}

func TestParseNotificationRefundCarriesRequestNo(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	headers, body := f.notification(t, map[string]string{
		"trade_no":       "ali-txn-9",
		"out_trade_no":   "PAY-9",
		"out_request_no": "RF-9",
		"refund_status":  "REFUND_SUCCESS",
		"refund_fee":     "0.40",
	})
	out, err := gw.ParseNotification(context.Background(), nil, headers, body)
	if err != nil {
		t.Fatalf("parse notification: %v", err)
	}
	if out.Kind != payentity.NotifyKindRefund || out.TargetNo != "RF-9" || out.RefundState != payentity.RefundSuccess {
		t.Fatalf("notification = %+v", out)
	}
	if out.RefundAmount != 40 {
		t.Fatalf("asserted refund amount = %d, want 40", out.RefundAmount)
	}
}

func TestParseNotificationRejectsBadSignature(t *testing.T) {
	f := newFakeAlipay(t)
	gw := f.gateway(t)
	_, body := f.notification(t, map[string]string{
		"trade_no": "ali-txn-x", "out_trade_no": "PAY-X", "trade_status": "TRADE_SUCCESS",
	})
	// Tamper: reorder/alter the body after signing.
	tampered := strings.Replace(string(body), "TRADE_SUCCESS", "TRADE_CLOSED", 1)
	if _, err := gw.ParseNotification(context.Background(), nil, nil, []byte(tampered)); err == nil {
		t.Fatal("forged notification accepted")
	}
}

func TestCertModeGatewayBuilds(t *testing.T) {
	f := newFakeAlipay(t)
	appCert := selfSignedPEM(t, f.appKey, 1)
	// The Alipay-side trust anchor the cert branch must actually load —
	// without it notifications carry no key material to verify against.
	alipayCert := selfSignedPEM(t, f.alipayKey, 2)
	rootCert := selfSignedPEM(t, f.alipayKey, 3)
	gw, err := newGateway(map[string]string{
		"app_id":             f.appID,
		"mode":               "cert",
		"app_private_key":    privateKeyPEM(t, f.appKey),
		"app_public_cert":    appCert,
		"alipay_public_cert": alipayCert,
		"alipay_root_cert":   rootCert,
	}, adapter.WithBaseURL(f.gatewayHost))
	if err != nil {
		t.Fatalf("cert mode gateway: %v", err)
	}
	if gw == nil || gw.Channel() != payentity.ChannelAlipay {
		t.Fatalf("gateway = %+v", gw)
	}
}

// ---- small helpers ----

func formOf(m map[string]string) url.Values {
	values := url.Values{}
	for k, v := range m {
		values.Set(k, v)
	}
	return values
}

func selfSignedPEM(t *testing.T, key *rsa.PrivateKey, serial int64) string {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: "fake-alipay-cert"},
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

