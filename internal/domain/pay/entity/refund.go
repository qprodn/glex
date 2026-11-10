package entity

import "time"

// RefundStatus is the Refund state machine: CAS from Pending, terminal
// states irreversible. Alipay's synchronous refund settles immediately
// through the same transitions.
type RefundStatus int16

const (
	RefundPending RefundStatus = 1
	RefundSuccess RefundStatus = 2
	RefundFailed  RefundStatus = 3
)

// Terminal reports whether the state is final.
func (s RefundStatus) Terminal() bool {
	return s == RefundSuccess || s == RefundFailed
}

// Refund is one request to return part or all of a paid Payment's amount.
// The ID doubles as the Refund number and the Channel's merchant refund
// number (out_refund_no). Together all refunds of a Payment never exceed
// what was paid. NotifyURL is the refund result address copied from the
// Pay Application at creation. Empty stays empty.
type Refund struct {
	ID              int64
	PaymentID       int64
	MerchantID      int64
	Channel         Channel
	NotifyURL       string
	Amount          int64
	Reason          string
	Status          RefundStatus
	ChannelRefundNo string
	FailReason      string
	RefundedAt      *time.Time
	CreateBy        int64
	UpdateBy        int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
