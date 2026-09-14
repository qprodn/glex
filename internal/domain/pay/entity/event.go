package entity

import "time"

// The module's Domain Events (ADR 0022): typed, json-tagged facts owned by
// the publishing module. The JSON shape is the cross-module contract —
// tags are part of it, fields evolve additively only, and subscribers
// decode into structs of their own.

// TopicPayEvents is the pay module's single Topic: few topics per module,
// the Event Type discriminates inside one (ADR 0022).
const TopicPayEvents = "pay.events"

// EventTypePaymentCompleted discriminates the Payment Completed fact.
const EventTypePaymentCompleted = "payment.completed"

// PaymentCompletedEvent asserts a Payment reached its paid terminal state.
// It is published once the state change is durable and carries the lookup
// keys plus the channel-asserted evidence; subscribers treat it as a
// prompt to look at the Payment, never as proof for moving money
// (ADR 0022).
type PaymentCompletedEvent struct {
	EventID        int64     `json:"event_id"`
	OccurredAt     time.Time `json:"occurred_at"`
	Type           string    `json:"type"`
	PaymentID      int64     `json:"payment_id"`
	MerchantID     int64     `json:"merchant_id"`
	Channel        string    `json:"channel"`
	BizNo          string    `json:"biz_no"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	ChannelTradeNo string    `json:"channel_trade_no"`
	PayerID        string    `json:"payer_id"`
	PaidAt         time.Time `json:"paid_at"`
}

// NewPaymentCompletedEvent assembles the event from the settled row and
// the channel notification that settled it. Amount is the row's own — the
// channel-asserted total was verified against it during the advance.
func NewPaymentCompletedEvent(eventID int64, p *Payment, notif *ChannelNotification) PaymentCompletedEvent {
	return PaymentCompletedEvent{
		Type:           EventTypePaymentCompleted,
		EventID:        eventID,
		OccurredAt:     time.Now(),
		PaymentID:      p.ID,
		MerchantID:     p.MerchantID,
		Channel:        p.Channel.Code(),
		BizNo:          p.BizNo,
		Amount:         p.Amount,
		Currency:       p.Currency,
		ChannelTradeNo: notif.ChannelTradeNo,
		PayerID:        notif.PayerID,
		PaidAt:         notif.PaidAt,
	}
}
