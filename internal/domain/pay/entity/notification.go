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
// doubles as the notify-log row's intake: the handler records the raw
// evidence (RawBody, SignHeaders, Verified) before any state advances.
type ChannelNotification struct {
	Channel   Channel
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
	// Payment success evidence.
	PayerID string
	PaidAt  time.Time
	// Refund outcome (Kind == NotifyKindRefund).
	RefundState RefundStatus
	RefundedAt  time.Time
	// Raw evidence, recorded write-once.
	RawBody     string
	SignHeaders map[string]string
	// Verified is true when the signature check passed; a false row is
	// evidence of a rejected delivery and never advances state.
	Verified bool
	// ReceivedAt is stamped by the store when the row lands.
	ReceivedAt time.Time
}
