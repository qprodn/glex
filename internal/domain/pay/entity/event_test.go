package entity

import (
	"encoding/json"
	"testing"
	"time"
)

// The event's JSON shape is the cross-module contract (ADR 0022): field
// names are frozen, new fields must be optional to old subscribers. This
// pins the tags so a rename cannot slip through review unseen.
func TestPaymentCompletedEventJSONShape(t *testing.T) {
	paidAt := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	occurredAt := time.Date(2026, 9, 14, 10, 0, 1, 0, time.UTC)
	e := NewPaymentCompletedEvent(7, &Payment{
		ID: 900, MerchantID: 7, Channel: ChannelWechat,
		BizNo: "ORDER-42", Amount: 100, Currency: "CNY",
	}, &ChannelNotification{
		ChannelTradeNo: "wx-trade-1", PayerID: "openid-x", PaidAt: paidAt,
	})
	e.OccurredAt = occurredAt

	body, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	want := `{"event_id":7,"occurred_at":"2026-09-14T10:00:01Z","type":"payment.completed",` +
		`"payment_id":900,"merchant_id":7,"channel":"wechat","biz_no":"ORDER-42","amount":100,` +
		`"currency":"CNY","channel_trade_no":"wx-trade-1","payer_id":"openid-x","paid_at":"2026-09-14T10:00:00Z"}`
	if string(body) != want {
		t.Fatalf("event JSON =\n%s\nwant\n%s", body, want)
	}
}
