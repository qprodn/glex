package entity

import "time"

// NotifyKind selects which stream a Channel Notification asserts about.
type NotifyKind int16

const (
	NotifyKindPayment NotifyKind = 1
	NotifyKindRefund  NotifyKind = 2
)

// ChannelNotification is the canonical shape every adapter parses a
// Channel push into after signature verification succeeds (ADR 0020). It
// doubles as the notify-log row: the intake records the raw evidence
// (RawBody, SignHeaders, Verified) before any state advances.
type ChannelNotification struct {
	// ID is the notify-log row's identity, minted by the intake path
	// before the write-once insert.
	ID         int64
	Channel    Channel
	MerchantID int64
	// NotifyID is the Channel's notification identifier — the dedup
	// half-key. Empty only for deliveries that failed parsing outright.
	NotifyID string
	Kind     NotifyKind
	// TargetNo is the Payment or Refund number the notification asserts
	// about (out_trade_no / out_refund_no).
	TargetNo string
	// ChannelTradeNo is the Channel's own trade/refund number, backfilled
	// onto the target row on success.
	ChannelTradeNo string
	// PaymentState is the channel-asserted payment state, normalized to
	// the module's vocabulary (Paid for success pushes; channels also
	// announce closes).
	PaymentState PaymentStatus
	// Amount is the channel-asserted payment total in minor units
	// (WeChat amount.total / Alipay total_amount). The advance path must
	// reject any notification whose Amount differs from Payment.Amount —
	// the last defense against a misrouted or mismatched settle.
	Amount int64
	// Payment success evidence.
	PayerID string
	PaidAt  time.Time
	// Refund outcome (Kind == NotifyKindRefund).
	RefundState RefundStatus
	RefundedAt  time.Time
	// RefundAmount is the channel-asserted refunded amount in minor units,
	// verified against Refund.Amount for Kind == NotifyKindRefund.
	RefundAmount int64
	// Raw evidence, recorded write-once.
	RawBody     string
	SignHeaders map[string]string
	// Verified is true when the signature check passed; a false row is
	// evidence of a rejected delivery and never advances state.
	Verified bool
	// ReceivedAt is stamped by the store when the row lands.
	ReceivedAt time.Time
}
