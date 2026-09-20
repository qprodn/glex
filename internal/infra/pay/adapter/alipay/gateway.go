package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"

	"github.com/smartwalle/alipay/v3"
)

// callTimeout bounds every outbound channel call (design §6: unified 10s
// channel-side timeout).
const callTimeout = 10 * time.Second

// beijingTime is the zone Alipay's text timestamps ("2006-01-02 15:04:05")
// are expressed in, independent of the host's TZ database.
var beijingTime = time.FixedZone("CST", 8*60*60)

// gateway is the alipay ChannelGateway. The official SDK is hidden here:
// request signing, response and notification verification, and the
// certificate-mode key rotation are all the SDK's job.
type gateway struct {
	client *alipay.Client
}

// newGateway builds the alipay gateway from a validated config value set.
// Both key mode and certificate mode are supported; certificate rotation
// is the SDK's concern (design §6).
func newGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	var o adapter.GatewayOptions
	for _, opt := range opts {
		opt(&o)
	}
	timeout := callTimeout
	if o.Timeout > 0 {
		timeout = o.Timeout
	}
	appID := strings.TrimSpace(cfg["app_id"])
	privateKey := cfg["app_private_key"]
	if appID == "" || privateKey == "" {
		return nil, errors.New("alipay: config is missing a required field")
	}
	// Always the production client — the sandbox host is never used; tests
	// override the endpoint explicitly.
	alipayOpts := []alipay.OptionFunc{}
	if o.BaseURL != "" {
		alipayOpts = append(alipayOpts, alipay.WithProductionGateway(o.BaseURL))
	}
	client, err := alipay.New(appID, privateKey, true, alipayOpts...)
	if err != nil {
		return nil, fmt.Errorf("alipay: client: %w", err)
	}
	client.Client = &http.Client{Timeout: timeout}
	switch cfg["mode"] {
	case "cert":
		// The certificate trio: the app public cert proves our identity,
		// the Alipay public cert is the verification trust anchor (async
		// notifications carry no serial to lazily bootstrap from), and
		// the root cert anchors the chain.
		if err := client.LoadAppCertPublicKey(cfg["app_public_cert"]); err != nil {
			return nil, fmt.Errorf("alipay: load app public cert: %w", err)
		}
		if err := client.LoadAlipayCertPublicKey(cfg["alipay_public_cert"]); err != nil {
			return nil, fmt.Errorf("alipay: load alipay public cert: %w", err)
		}
		if err := client.LoadAliPayRootCert(cfg["alipay_root_cert"]); err != nil {
			return nil, fmt.Errorf("alipay: load root cert: %w", err)
		}
	default: // "key" — the schema default
		if err := client.LoadAliPayPublicKey(cfg["alipay_public_key"]); err != nil {
			return nil, fmt.Errorf("alipay: load alipay public key: %w", err)
		}
	}
	return &gateway{client: client}, nil
}

// ---- ChannelGateway ----

func (g *gateway) Channel() payentity.Channel { return payentity.ChannelAlipay }

// Prepay runs the 当面付 precreate (Native scan): the channel answers with
// the QR payload. The minor-unit amount is converted to Alipay's yuan
// string here — the only unit conversion the adapter does (ADR 0019).
func (g *gateway) Prepay(ctx context.Context, req payservice.PrepayRequest) (payservice.PrepayResult, error) {
	if req.Method != payentity.PayMethodNative {
		return payservice.PrepayResult{}, fmt.Errorf("alipay: unsupported pay method %d", req.Method)
	}
	var p alipay.TradePreCreate
	p.NotifyURL = req.NotifyURL
	p.OutTradeNo = req.PaymentNo
	p.Subject = req.Subject
	p.TotalAmount = fenToYuan(req.Amount)
	if !req.ExpireAt.IsZero() {
		if minutes := int(time.Until(req.ExpireAt).Minutes()); minutes >= 1 {
			p.TimeoutExpress = fmt.Sprintf("%dm", minutes)
		}
	}
	rsp, err := g.client.TradePreCreate(ctx, p)
	if err != nil {
		return payservice.PrepayResult{}, err
	}
	if !rsp.IsSuccess() {
		return payservice.PrepayResult{}, fmt.Errorf("alipay: %s: %s", rsp.SubCode, rsp.SubMsg)
	}
	return payservice.PrepayResult{ChannelPayload: rsp.QRCode}, nil
}

// Query asks the channel for the payment's true state.
func (g *gateway) Query(ctx context.Context, req payservice.QueryRequest) (payservice.ChannelPaymentState, error) {
	var q alipay.TradeQuery
	q.OutTradeNo = req.PaymentNo
	rsp, err := g.client.TradeQuery(ctx, q)
	if err != nil {
		return payservice.ChannelPaymentState{}, err
	}
	if !rsp.IsSuccess() {
		// A trade the channel has not recorded yet is a pending payment,
		// not a failed query (precreate → instant query races).
		if rsp.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return payservice.ChannelPaymentState{State: payentity.PaymentPending}, nil
		}
		return payservice.ChannelPaymentState{}, fmt.Errorf("alipay: %s: %s", rsp.SubCode, rsp.SubMsg)
	}
	state := payservice.ChannelPaymentState{
		State:          mapTradeStatus(rsp.TradeStatus),
		ChannelTradeNo: rsp.TradeNo,
		Amount:         parseYuanToFen(rsp.TotalAmount),
	}
	if rsp.BuyerLogonId != "" {
		state.PayerID = rsp.BuyerLogonId
	} else if rsp.BuyerUserId != "" {
		state.PayerID = rsp.BuyerUserId
	}
	if t := parseAlipayTime(rsp.SendPayDate); t != nil {
		state.PaidAt = *t
	}
	return state, nil
}

// Close closes the order; a status-refusal surfaces as
// ErrChannelAlreadyPaid so the settlement path can flip into a query.
func (g *gateway) Close(ctx context.Context, req payservice.CloseRequest) error {
	var p alipay.TradeClose
	p.OutTradeNo = req.PaymentNo
	rsp, err := g.client.TradeClose(ctx, p)
	if err != nil {
		return err
	}
	if rsp.IsSuccess() {
		return nil
	}
	switch rsp.SubCode {
	case "ACQ.TRADE_STATUS_ERROR":
		// The trade is past the closable state — paid, most likely.
		return fmt.Errorf("%w: alipay: %s: %s", payservice.ErrChannelAlreadyPaid, rsp.SubCode, rsp.SubMsg)
	case "ACQ.TRADE_NOT_EXIST":
		// Nothing to close at the channel; locally-owned decision follows.
		return nil
	}
	return fmt.Errorf("alipay: %s: %s", rsp.SubCode, rsp.SubMsg)
}

// Refund requests one refund. Alipay's 当面付 refund answers synchronously —
// the returned state is already settled (design §5).
func (g *gateway) Refund(ctx context.Context, req payservice.RefundRequest) (payservice.ChannelRefundState, error) {
	var p alipay.TradeRefund
	p.OutTradeNo = req.PaymentNo
	p.OutRequestNo = req.RefundNo
	p.RefundAmount = fenToYuan(req.Amount)
	p.RefundReason = req.Reason
	rsp, err := g.client.TradeRefund(ctx, p)
	if err != nil {
		return payservice.ChannelRefundState{}, err
	}
	if !rsp.IsSuccess() {
		// A hard refusal can never succeed on a retry — fail terminally.
		// Anything else (system errors, balance races, unknown codes)
		// returns an error so the row stays Pending for an idempotent
		// same-out_request_no retry: marking an unknown outcome Failed
		// risks double-paying a refund that actually succeeded.
		if isHardRefundRejection(rsp.SubCode) {
			return payservice.ChannelRefundState{
				State:      payentity.RefundFailed,
				FailReason: fmt.Sprintf("%s: %s", rsp.SubCode, rsp.SubMsg),
			}, nil
		}
		return payservice.ChannelRefundState{}, fmt.Errorf("alipay: refund outcome inconclusive (%s: %s)", rsp.SubCode, rsp.SubMsg)
	}
	// Synchronous settlement — FundChange "N" means the identical refund
	// request was already processed, which is still a settled refund. The
	// settled amount is exactly the one the channel just accepted (an
	// amount mismatch is rejected with ACQ.REFUND_AMT_NOT_EQUAL above).
	return payservice.ChannelRefundState{
		State:        payentity.RefundSuccess,
		RefundedAt:   time.Now(),
		RefundAmount: req.Amount,
	}, nil
}

// isHardRefundRejection reports whether the SubCode is a permanent refusal
// the channel can never settle on a retry — the only outcomes a refund may
// be terminally Failed on. Alipay's guidance for transient codes (system
// errors, seller-balance races, status races) is to re-issue with the same
// parameters, which the PENDING row preserves.
func isHardRefundRejection(subCode string) bool {
	switch subCode {
	case "ACQ.TRADE_NOT_EXIST", "ACQ.TRADE_HAS_CLOSED", "ACQ.TRADE_NOT_OPEN",
		"ACQ.REFUND_AMT_NOT_EQUAL", "ACQ.REFUND_NOT_ENOUGH", "ACQ.INVALID_PARAMETER":
		return true
	}
	return strings.HasPrefix(subCode, "ACQ.PERMISSION_")
}

// QueryRefund asks the channel for the refund's true state. The API
// requires the owning payment's out_trade_no alongside the out_request_no
// — sending only the refund number is rejected as a parameter error by the
// real gateway.
func (g *gateway) QueryRefund(ctx context.Context, req payservice.RefundQueryRequest) (payservice.ChannelRefundState, error) {
	var p alipay.TradeFastPayRefundQuery
	p.OutTradeNo = req.PaymentNo
	p.OutRequestNo = req.RefundNo
	rsp, err := g.client.TradeFastPayRefundQuery(ctx, p)
	if err != nil {
		return payservice.ChannelRefundState{}, err
	}
	if !rsp.IsSuccess() {
		// A refund the channel never accepted (or whose trade is unknown)
		// is not an outcome — it may still be issued. Every other query
		// failure is an error the caller retries: a query cannot assert a
		// terminal state it did not observe.
		if rsp.SubCode == "ACQ.REFUND_NOT_EXIST" || rsp.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return payservice.ChannelRefundState{State: payentity.RefundPending}, nil
		}
		return payservice.ChannelRefundState{}, fmt.Errorf("alipay: %s: %s", rsp.SubCode, rsp.SubMsg)
	}
	state := payservice.ChannelRefundState{State: payentity.RefundPending}
	if rsp.RefundStatus == "REFUND_SUCCESS" {
		state.State = payentity.RefundSuccess
		state.RefundAmount = parseYuanToFen(rsp.RefundAmount)
		if t := parseAlipayTime(rsp.GMTRefundPay); t != nil {
			state.RefundedAt = *t
		}
	}
	return state, nil
}

// ParseNotification verifies the delivery's signature against the loaded
// Alipay key material, then normalizes it into the canonical event. Any
// verification failure is an error — the caller records the unverified
// evidence and rejects (ADR 0020).
func (g *gateway) ParseNotification(ctx context.Context, req payservice.NotificationRequest) (payentity.ChannelNotification, error) {
	body := req.Body
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return payentity.ChannelNotification{}, fmt.Errorf("alipay: unreadable notification body")
	}
	notif, err := g.client.DecodeNotification(ctx, values)
	if err != nil {
		return payentity.ChannelNotification{}, err
	}
	out := payentity.ChannelNotification{
		Channel:        payentity.ChannelAlipay,
		NotifyID:       notif.NotifyId,
		Verified:       true,
		ChannelTradeNo: notif.TradeNo,
		PayerID:        notif.BuyerLogonId,
	}
	if t := parseAlipayTime(notif.GmtPayment); t != nil {
		out.PaidAt = *t
	}
	// Refund deliveries carry the refund request number; payment
	// deliveries carry only the trade number.
	if notif.OutRequestNo != "" {
		out.Kind = payentity.NotifyKindRefund
		out.TargetNo = notif.OutRequestNo
		out.RefundState = payentity.RefundSuccess
		out.RefundedAt = time.Now()
		out.RefundAmount = parseYuanToFen(notif.RefundFee)
	} else {
		out.Kind = payentity.NotifyKindPayment
		out.TargetNo = notif.OutTradeNo
		out.PaymentState = mapTradeStatus(notif.TradeStatus)
		out.Amount = parseYuanToFen(notif.TotalAmount)
	}
	return out, nil
}

// Probe runs the Connectivity Test: querying a surely-nonexistent trade
// succeeds exactly when the channel answers ACQ.TRADE_NOT_EXIST with a
// verifiable response — the signature and verification chain is the thing
// being proven (design §7).
func (g *gateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error {
	probeNo := fmt.Sprintf("probe%d", time.Now().UnixNano())
	var p alipay.TradeQuery
	p.OutTradeNo = probeNo
	rsp, err := g.client.TradeQuery(ctx, p)
	if err != nil {
		return err
	}
	if rsp.SubCode == "ACQ.TRADE_NOT_EXIST" {
		return nil
	}
	if rsp.IsSuccess() {
		// Cannot happen for a probe-order; treat a chatty sandbox as
		// healthy anyway.
		return nil
	}
	return fmt.Errorf("alipay: %s: %s", rsp.SubCode, rsp.SubMsg)
}

// ---- helpers ----

// mapTradeStatus normalizes the channel's trade_status. TRADE_FINISHED is
// a paid (and fully settled, non-refundable) payment — PAID for the
// payment status machine.
func mapTradeStatus(s alipay.TradeStatus) payentity.PaymentStatus {
	switch s {
	case alipay.TradeStatusSuccess, alipay.TradeStatusFinished:
		return payentity.PaymentPaid
	case alipay.TradeStatusClosed:
		return payentity.PaymentClosed
	default: // WAIT_BUYER_PAY, unknown
		return payentity.PaymentPending
	}
}

// fenToYuan renders minor units as Alipay's two-decimal yuan string with
// integer math only — floats never touch amounts (ADR 0019).
func fenToYuan(fen int64) string {
	if fen < 0 {
		fen = 0
	}
	return fmt.Sprintf("%d.%02d", fen/100, fen%100)
}

// parseYuanToFen converts Alipay's two-decimal yuan text into minor units
// with integer math only (ADR 0019). An unparseable amount yields 0, which
// the advance path treats as "not asserted" and skips verification on.
func parseYuanToFen(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	yuan, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		yuan, frac = s[:i], s[i+1:]
		if len(frac) > 2 {
			return 0
		}
		for len(frac) < 2 {
			frac += "0"
		}
	}
	y, errYuan := strconv.ParseInt(yuan, 10, 64)
	f, errFrac := strconv.ParseInt(frac, 10, 64)
	if errYuan != nil || errFrac != nil {
		return 0
	}
	return y*100 + f
}

// parseAlipayTime parses the channel's Beijing-time text timestamps.
func parseAlipayTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, beijingTime)
	if err != nil {
		return nil
	}
	return &t
}
