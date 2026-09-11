// Package model holds the pay contract's DTOs — the shapes a consuming
// business module sees, decoupled from both the proto wire types and the
// domain entities.
package model

import "time"

// Payment is the contract's view of one payment (支付单).
type Payment struct {
	// PaymentNo is the payment's number — also the channel's
	// out_trade_no.
	PaymentNo  int64
	MerchantID int64
	// Channel: 1 WeChat Pay, 2 Alipay (entity.Channel's codes).
	Channel int
	BizNo   string
	Subject string
	// Amount is int64 minor units, always (ADR 0019).
	Amount         int64
	Currency       string
	PayMethod      int
	Status         int
	ChannelTradeNo string
	// ChannelPayload carries the method-specific render input (Native:
	// the code_url a QR encodes).
	ChannelPayload string
	PayerID        string
	RefundedAmount int64
	FailReason     string
	ExpireTime     time.Time
	PaidAt         *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PaymentRequest is what an in-process initiation states.
type PaymentRequest struct {
	MerchantID int64
	BizNo      string
	Subject    string
	// Amount is int64 minor units (fen), > 0 (ADR 0019).
	Amount        int64
	PayMethod     int
	ExpireMinutes int32
}
