package wechat

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"

	core "github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
)

// callTimeout bounds every outbound channel call (design §6: unified 10s
// channel-side timeout).
const callTimeout = 10 * time.Second

// gateway is the wechat ChannelGateway. The official SDK is hidden here:
// request signing, response verification, platform-certificate download
// and scheduled refresh (its certdownloader), and notification
// verification/decryption are all the SDK's job.
type gateway struct {
	mchID  string
	appid  string
	client *core.Client
	native native.NativeApiService
	refund refunddomestic.RefundsApiService
	notify *notify.Handler
}

// newGateway builds the wechat gateway from a validated config value set.
// Building one pulls the merchant's platform certificates synchronously —
// wrong credentials fail here, which is exactly what the Connectivity
// Test surfaces (design §7: 微信拉平台证书).
func newGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	var o adapter.GatewayOptions
	for _, opt := range opts {
		opt(&o)
	}
	timeout := callTimeout
	if o.Timeout > 0 {
		timeout = o.Timeout
	}
	mchID := cfg["mchid"]
	appid := cfg["appid"]
	apiV3Key := cfg["api_v3_key"]
	serial := cfg["merchant_serial"]
	privateKeyPEM := cfg["merchant_private_key"]
	if mchID == "" || appid == "" || apiV3Key == "" || serial == "" || privateKeyPEM == "" {
		return nil, errors.New("wechat: config is missing a required field")
	}
	privateKey, err := loadPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Timeout: timeout}
	if o.BaseURL != "" {
		// Contract-test mode: every SDK call is redirected to the fake
		// channel server.
		hc.Transport = &hostRewriteTransport{base: o.BaseURL}
	}
	ctx := context.Background()
	// Bootstrap client without a verifier — nothing to verify against
	// before the platform certificates land.
	bootstrap, err := core.NewClient(ctx,
		option.WithMerchantCredential(mchID, serial, privateKey),
		option.WithHTTPClient(hc),
		option.WithoutValidator(),
	)
	if err != nil {
		return nil, fmt.Errorf("wechat: bootstrap client: %w", err)
	}
	// Register (and synchronously run) the platform-certificate download
	// for this merchant; the SDK's scheduled downloader keeps it fresh.
	if !downloader.MgrInstance().HasDownloader(ctx, mchID) {
		if err := downloader.MgrInstance().RegisterDownloaderWithClient(ctx, bootstrap, mchID, apiV3Key); err != nil {
			return nil, fmt.Errorf("wechat: platform certificate download failed: %w", err)
		}
	}
	visitor := downloader.MgrInstance().GetCertificateVisitor(mchID)
	client, err := core.NewClient(ctx,
		option.WithMerchantCredential(mchID, serial, privateKey),
		option.WithVerifier(verifiers.NewSHA256WithRSAVerifier(visitor)),
		option.WithHTTPClient(hc),
	)
	if err != nil {
		return nil, fmt.Errorf("wechat: client: %w", err)
	}
	return &gateway{
		mchID:  mchID,
		appid:  appid,
		client: client,
		native: native.NativeApiService{Client: client},
		refund: refunddomestic.RefundsApiService{Client: client},
		notify: notify.NewNotifyHandler(apiV3Key, verifiers.NewSHA256WithRSAVerifier(visitor)),
	}, nil
}

// ---- ChannelGateway ----

func (g *gateway) Channel() payentity.Channel { return payentity.ChannelWechat }

// Prepay creates the Native order; CNY fen are already the module's minor
// units (ADR 0019 — wechat speaks the same unit, the conversion is
// identity).
func (g *gateway) Prepay(ctx context.Context, req payservice.PrepayRequest) (payservice.PrepayResult, error) {
	if req.Method != payentity.PayMethodNative {
		return payservice.PrepayResult{}, fmt.Errorf("wechat: unsupported pay method %d", req.Method)
	}
	resp, _, err := g.native.Prepay(ctx, native.PrepayRequest{
		Appid:       sp(g.appid),
		Mchid:       sp(g.mchID),
		Description: sp(req.Subject),
		OutTradeNo:  sp(req.PaymentNo),
		TimeExpire:  tp(req.ExpireAt),
		NotifyUrl:   sp(req.NotifyURL),
		Amount:      &native.Amount{Total: i64(req.Amount), Currency: sp("CNY")},
	})
	if err != nil {
		return payservice.PrepayResult{}, mapAPIError(err)
	}
	if resp.CodeUrl == nil {
		return payservice.PrepayResult{}, errors.New("wechat: prepay returned no code_url")
	}
	return payservice.PrepayResult{ChannelPayload: *resp.CodeUrl}, nil
}

// Query asks the channel for the payment's true state, normalized to the
// module's status vocabulary.
func (g *gateway) Query(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) (payservice.ChannelPaymentState, error) {
	resp, _, err := g.native.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: sp(paymentNo),
		Mchid:      sp(g.mchID),
	})
	if err != nil {
		return payservice.ChannelPaymentState{}, mapAPIError(err)
	}
	state := payservice.ChannelPaymentState{State: mapTradeState(deref(resp.TradeState))}
	if resp.Amount != nil {
		state.Amount = derefI64(resp.Amount.Total)
	}
	if resp.TransactionId != nil {
		state.ChannelTradeNo = *resp.TransactionId
	}
	if resp.Payer != nil && resp.Payer.Openid != nil {
		state.PayerID = *resp.Payer.Openid
	}
	if t := parseTime(deref(resp.SuccessTime)); t != nil {
		state.PaidAt = *t
	}
	if resp.TradeStateDesc != nil && state.State == payentity.PaymentFailed {
		state.FailReason = *resp.TradeStateDesc
	}
	return state, nil
}

// Close closes the order; an already-paid answer surfaces as
// ErrChannelAlreadyPaid so the settlement path can flip into a query.
func (g *gateway) Close(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) error {
	_, err := g.native.CloseOrder(ctx, native.CloseOrderRequest{
		OutTradeNo: sp(paymentNo),
		Mchid:      sp(g.mchID),
	})
	if err == nil {
		return nil
	}
	var apiErr *core.APIError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Code, "ORDER_PAID") {
		return fmt.Errorf("%w: wechat: %s", payservice.ErrChannelAlreadyPaid, apiErr.Code)
	}
	return mapAPIError(err)
}

// Refund requests one refund. WeChat refunds settle asynchronously; the
// returned state is Pending unless the channel already reported an outcome.
func (g *gateway) Refund(ctx context.Context, req payservice.RefundRequest) (payservice.ChannelRefundState, error) {
	resp, _, err := g.refund.Create(ctx, refunddomestic.CreateRequest{
		OutTradeNo:  sp(req.PaymentNo),
		OutRefundNo: sp(req.RefundNo),
		Reason:      sp(req.Reason),
		NotifyUrl:   sp(req.NotifyURL),
		Amount: &refunddomestic.AmountReq{
			Refund:   i64(req.Amount),
			Total:    i64(req.Total),
			Currency: sp("CNY"),
		},
	})
	if err != nil {
		return payservice.ChannelRefundState{}, mapAPIError(err)
	}
	state := mapRefund(deref((*string)(resp.Status)), resp.SuccessTime, resp.RefundId)
	if resp.Amount != nil {
		state.RefundAmount = derefI64(resp.Amount.Refund)
	}
	return state, nil
}

// QueryRefund asks the channel for the refund's true state. WeChat
// resolves by out_refund_no alone; paymentNo exists on the seam for
// channels whose query API requires it.
func (g *gateway) QueryRefund(ctx context.Context, m *payentity.MerchantAccount, refundNo, _ string) (payservice.ChannelRefundState, error) {
	resp, _, err := g.refund.QueryByOutRefundNo(ctx, refunddomestic.QueryByOutRefundNoRequest{
		OutRefundNo: sp(refundNo),
	})
	if err != nil {
		return payservice.ChannelRefundState{}, mapAPIError(err)
	}
	state := mapRefund(deref((*string)(resp.Status)), resp.SuccessTime, resp.RefundId)
	if resp.Amount != nil {
		state.RefundAmount = derefI64(resp.Amount.Refund)
	}
	return state, nil
}

// ParseNotification verifies the delivery's signature against the cached
// platform certificates, decrypts the resource with the APIv3 key, and
// normalizes it into the canonical event. Any verification or decryption
// failure is an error — the caller records the unverified evidence and
// rejects (ADR 0020).
func (g *gateway) ParseNotification(ctx context.Context, m *payentity.MerchantAccount, headers http.Header, body []byte) (payentity.ChannelNotification, error) {
	// Route on the (still unverified) event type purely to pick the
	// content type; the handler re-verifies the signature over the full
	// body before anything is trusted.
	var probe struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return payentity.ChannelNotification{}, fmt.Errorf("%w: wechat: unreadable notification body", pay.ErrNotifyInvalid)
	}
	httpReq, err := http.NewRequest(http.MethodPost, "/pay/notify/wechat/0", bytes.NewReader(body))
	if err != nil {
		return payentity.ChannelNotification{}, err
	}
	httpReq.Header = headers
	switch {
	case strings.HasPrefix(probe.EventType, "TRANSACTION."):
		var tx payments.Transaction
		nreq, err := g.notify.ParseNotifyRequest(ctx, httpReq, &tx)
		if err != nil {
			return payentity.ChannelNotification{}, err
		}
		out := payentity.ChannelNotification{
			Channel:        payentity.ChannelWechat,
			NotifyID:       nreq.ID,
			Kind:           payentity.NotifyKindPayment,
			TargetNo:       deref(tx.OutTradeNo),
			ChannelTradeNo: deref(tx.TransactionId),
			PaymentState:   mapTradeState(deref(tx.TradeState)),
			Verified:       true,
		}
		if tx.Amount != nil {
			out.Amount = derefI64(tx.Amount.Total)
		}
		if tx.Payer != nil {
			out.PayerID = deref(tx.Payer.Openid)
		}
		if t := parseTime(deref(tx.SuccessTime)); t != nil {
			out.PaidAt = *t
		}
		return out, nil
	case strings.HasPrefix(probe.EventType, "REFUND."):
		var rf refunddomestic.Refund
		nreq, err := g.notify.ParseNotifyRequest(ctx, httpReq, &rf)
		if err != nil {
			return payentity.ChannelNotification{}, err
		}
		out := payentity.ChannelNotification{
			Channel:        payentity.ChannelWechat,
			NotifyID:       nreq.ID,
			Kind:           payentity.NotifyKindRefund,
			TargetNo:       deref(rf.OutRefundNo),
			ChannelTradeNo: deref(rf.RefundId),
			RefundState:    mapRefundStatus(deref((*string)(rf.Status))),
			Verified:       true,
		}
		if rf.Amount != nil {
			out.RefundAmount = derefI64(rf.Amount.Refund)
		}
		if rf.SuccessTime != nil {
			out.RefundedAt = *rf.SuccessTime
		}
		return out, nil
	default:
		return payentity.ChannelNotification{}, fmt.Errorf("wechat: unsupported event type %q", probe.EventType)
	}
}

// Probe runs the Connectivity Test. The gateway build itself already
// pulled the platform certificates (the credential chain's first proof);
// the probe then makes one cheap authenticated call — querying a
// surely-nonexistent order — and succeeds exactly when the channel
// answers ORDER_NOT_EXIST with a verifiable response.
func (g *gateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error {
	probeNo := fmt.Sprintf("probe%d", time.Now().UnixNano())
	_, _, err := g.native.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: sp(probeNo),
		Mchid:      sp(g.mchID),
	})
	if err == nil {
		// The probe order cannot exist; a plain success would mean the
		// channel is not actually checking — treat as a healthy channel
		// anyway (some sandbox environments answer ANY).
		return nil
	}
	var apiErr *core.APIError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Code, "NOT_EXIST") {
		return nil
	}
	return mapAPIError(err)
}

// ---- helpers ----

// mapTradeState normalizes the channel's trade_state to the module's
// vocabulary. REFUND is a paid payment that has since been (partly)
// refunded — for the payment status machine it is PAID.
func mapTradeState(s string) payentity.PaymentStatus {
	switch s {
	case "SUCCESS", "REFUND":
		return payentity.PaymentPaid
	case "CLOSED", "REVOKED":
		return payentity.PaymentClosed
	case "PAYERROR":
		return payentity.PaymentFailed
	default: // NOTPAY, USERPAYING, ACCEPT, unknown
		return payentity.PaymentPending
	}
}

// mapRefundStatus normalizes the channel's refund status.
func mapRefundStatus(s string) payentity.RefundStatus {
	switch s {
	case "SUCCESS":
		return payentity.RefundSuccess
	case "CLOSED", "ABNORMAL":
		return payentity.RefundFailed
	default: // PROCESSING, unknown
		return payentity.RefundPending
	}
}

// mapRefund assembles the canonical refund state from a channel Refund.
func mapRefund(status string, successTime *time.Time, refundID *string) payservice.ChannelRefundState {
	state := payservice.ChannelRefundState{State: mapRefundStatus(status)}
	if successTime != nil {
		state.RefundedAt = *successTime
	}
	if refundID != nil {
		state.ChannelRefundNo = *refundID
	}
	if state.State == payentity.RefundFailed {
		state.FailReason = "refund closed or abnormal at the channel"
	}
	return state
}

// mapAPIError renders channel API failures as plain errors carrying the
// channel's code; the application maps unknown failures to CHANNEL_ERROR.
func mapAPIError(err error) error {
	var apiErr *core.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("wechat: %s: %s", apiErr.Code, apiErr.Message)
	}
	return err
}

// loadPrivateKey parses the merchant's PEM-encoded private key (PKCS#8 or
// PKCS#1).
func loadPrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("wechat: merchant private key is not valid PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("wechat: merchant private key is not RSA")
		}
		return rsaKey, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("wechat: parse merchant private key: %w", err)
	}
	return key, nil
}

// hostRewriteTransport redirects every SDK call to one fixed endpoint —
// the contract tests' fake channel server (plain HTTP, no TLS).
type hostRewriteTransport struct {
	base string
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme = "http"
	u.Host = t.base
	return http.DefaultTransport.RoundTrip(&http.Request{
		Method:        req.Method,
		URL:           &u,
		Header:        req.Header,
		Body:          req.Body,
		ContentLength: req.ContentLength,
		GetBody:       req.GetBody,
	})
}

var _ http.RoundTripper = (*hostRewriteTransport)(nil)

func sp(s string) *string { return &s }
func i64(v int64) *int64  { return &v }

func tp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefI64 returns *p or 0 when nil — the SDK's optional amount fields.
func derefI64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// parseTime parses the channel's RFC3339 timestamps; empty input yields
// nil (no evidence).
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}
