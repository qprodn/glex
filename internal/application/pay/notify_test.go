package pay

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"glex/internal/conf"
	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/eventbus"
)

// The notify advance's merchant binding: a signature-verified notification
// proves the channel signed an event for the URL-merchant's account, but the
// asserted out_trade_no/out_refund_no is attacker-choosable within that
// account — so the advance must never settle a row owned by another
// merchant (or carried on another channel).

type advancePaymentSvc struct {
	payservice.Payment
	row     *payentity.Payment
	getErr  error
	applied int
	// absorb makes the CAS report "no transition" (a redelivery).
	absorb bool
}

func (f *advancePaymentSvc) Get(ctx context.Context, id int64) (*payentity.Payment, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.row, nil
}

func (f *advancePaymentSvc) ApplyChannelState(ctx context.Context, req payservice.ApplyChannelStateRequest) (bool, error) {
	f.applied++
	if f.absorb {
		return false, nil
	}
	return true, nil
}

// fakeEventProducer records what the application published; err (when set)
// degrades every send so the best-effort posture can be asserted.
type fakeEventProducer struct {
	sent [][]byte
	err  error
}

func (f *fakeEventProducer) Send(ctx context.Context, body []byte, opts ...eventbus.SendOpt) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, body)
	return nil
}

func (f *fakeEventProducer) BatchSend(ctx context.Context, bodyArr [][]byte, opts ...eventbus.SendOpt) error {
	for _, body := range bodyArr {
		if err := f.Send(ctx, body, opts...); err != nil {
			return err
		}
	}
	return nil
}

func newAdvanceTestApp(payments payservice.Payment, refunds payservice.Refund, events eventbus.Producer) *PayApplication {
	return NewPayApplication(
		&fakeMerchantSvc{},
		payments,
		refunds,
		&fakeNotificationSvc{},
		&fakeTx{},
		&fakeLocker{},
		events,
		&conf.Pay{},
		slog.New(slog.DiscardHandler),
	)
}

type advanceRefundSvc struct {
	payservice.Refund
	row     *payentity.Refund
	getErr  error
	applied int
}

func (f *advanceRefundSvc) Get(ctx context.Context, id int64) (*payentity.Refund, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.row, nil
}

func (f *advanceRefundSvc) ApplyRefundState(ctx context.Context, req payservice.ApplyRefundStateRequest) (bool, error) {
	f.applied++
	return true, nil
}

func TestAdvanceRejectsCrossMerchantTarget(t *testing.T) {
	payments := &advancePaymentSvc{row: &payentity.Payment{ID: 900, MerchantID: 2, Channel: payentity.ChannelWechat}}
	refunds := &advanceRefundSvc{row: &payentity.Refund{ID: 901, MerchantID: 2, Channel: payentity.ChannelWechat}}
	app := newAdvanceTestApp(payments, refunds, &fakeEventProducer{})

	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	}
	advanced, err := app.advance(context.Background(), notif)
	if err != nil || advanced {
		t.Fatalf("payment advance = (%v, %v), want (false, nil)", advanced, err)
	}
	if payments.applied != 0 {
		t.Fatal("cross-merchant payment notification advanced the target")
	}

	notif.Kind = payentity.NotifyKindRefund
	notif.TargetNo = "901"
	notif.RefundState = payentity.RefundSuccess
	advanced, err = app.advance(context.Background(), notif)
	if err != nil || advanced {
		t.Fatalf("refund advance = (%v, %v), want (false, nil)", advanced, err)
	}
	if refunds.applied != 0 {
		t.Fatal("cross-merchant refund notification advanced the target")
	}
}

func TestAdvanceRejectsCrossChannelTarget(t *testing.T) {
	payments := &advancePaymentSvc{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelAlipay}}
	app := newAdvanceTestApp(payments, &advanceRefundSvc{}, &fakeEventProducer{})
	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	}
	advanced, err := app.advance(context.Background(), notif)
	if err != nil || advanced {
		t.Fatalf("advance = (%v, %v), want (false, nil)", advanced, err)
	}
	if payments.applied != 0 {
		t.Fatal("cross-channel notification advanced the target")
	}
}

func TestAdvanceAcceptsOwnedTarget(t *testing.T) {
	payments := &advancePaymentSvc{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	app := newAdvanceTestApp(payments, &advanceRefundSvc{}, &fakeEventProducer{})
	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	}
	advanced, err := app.advance(context.Background(), notif)
	if err != nil || !advanced {
		t.Fatalf("advance = (%v, %v), want (true, nil)", advanced, err)
	}
	if payments.applied != 1 {
		t.Fatalf("applied = %d, want 1", payments.applied)
	}
}

func TestAdvanceUnmatchableTargetIsAbsorbed(t *testing.T) {
	payments := &advancePaymentSvc{getErr: pay.ErrPaymentNotFound}
	refunds := &advanceRefundSvc{getErr: pay.ErrRefundNotFound}
	app := newAdvanceTestApp(payments, refunds, &fakeEventProducer{})

	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	}
	advanced, err := app.advance(context.Background(), notif)
	if err != nil || advanced {
		t.Fatalf("payment advance = (%v, %v), want (false, nil)", advanced, err)
	}

	notif.Kind = payentity.NotifyKindRefund
	notif.RefundState = payentity.RefundSuccess
	advanced, err = app.advance(context.Background(), notif)
	if err != nil || advanced {
		t.Fatalf("refund advance = (%v, %v), want (false, nil)", advanced, err)
	}
}

func TestAdvancePublishesPaymentCompleted(t *testing.T) {
	paidAt := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	payments := &advancePaymentSvc{row: &payentity.Payment{
		ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat,
		BizNo: "ORDER-42", Amount: 100, Currency: "CNY",
	}}
	events := &fakeEventProducer{}
	app := newAdvanceTestApp(payments, &advanceRefundSvc{}, events)

	notif := &payentity.ChannelNotification{
		Kind:           payentity.NotifyKindPayment,
		Channel:        payentity.ChannelWechat,
		MerchantID:     7,
		TargetNo:       "900",
		PaymentState:   payentity.PaymentPaid,
		ChannelTradeNo: "wx-trade-1",
		PayerID:        "openid-x",
		PaidAt:         paidAt,
		Amount:         100,
	}
	if advanced, err := app.advance(context.Background(), notif); err != nil || !advanced {
		t.Fatalf("advance = (%v, %v), want (true, nil)", advanced, err)
	}
	if len(events.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(events.sent))
	}
	var got payentity.PaymentCompletedEvent
	if err := json.Unmarshal(events.sent[0], &got); err != nil {
		t.Fatalf("published payload is not the event JSON: %v", err)
	}
	want := payentity.PaymentCompletedEvent{
		Type:           payentity.EventTypePaymentCompleted,
		EventID:        1,
		PaymentID:      900,
		MerchantID:     7,
		Channel:        "wechat",
		BizNo:          "ORDER-42",
		Amount:         100,
		Currency:       "CNY",
		ChannelTradeNo: "wx-trade-1",
		PayerID:        "openid-x",
		PaidAt:         paidAt,
	}
	got.OccurredAt = want.OccurredAt // timestamps differ by construction
	if got != want {
		t.Fatalf("published event = %+v, want %+v", got, want)
	}
}

func TestAdvanceAbsorbedDuplicateDoesNotRepublish(t *testing.T) {
	payments := &advancePaymentSvc{
		row:    &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat},
		absorb: true,
	}
	events := &fakeEventProducer{}
	app := newAdvanceTestApp(payments, &advanceRefundSvc{}, events)

	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	}
	if advanced, err := app.advance(context.Background(), notif); err != nil || advanced {
		t.Fatalf("advance = (%v, %v), want (false, nil)", advanced, err)
	}
	if len(events.sent) != 0 {
		t.Fatalf("absorbed duplicate published %d events, want 0", len(events.sent))
	}
}

func TestAdvanceNonPaidTransitionDoesNotPublish(t *testing.T) {
	payments := &advancePaymentSvc{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	events := &fakeEventProducer{}
	app := newAdvanceTestApp(payments, &advanceRefundSvc{}, events)

	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentClosed,
	}
	if advanced, err := app.advance(context.Background(), notif); err != nil || !advanced {
		t.Fatalf("advance = (%v, %v), want (true, nil)", advanced, err)
	}
	if len(events.sent) != 0 {
		t.Fatalf("non-paid transition published %d events, want 0", len(events.sent))
	}
}

func TestPublishDegradationDoesNotFailIntake(t *testing.T) {
	payments := &advancePaymentSvc{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	events := &fakeEventProducer{err: errors.New("queue full")}
	app := newAdvanceTestApp(payments, &advanceRefundSvc{}, events)

	notif := &payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		MerchantID:   7,
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	}
	// The publish degraded, but the advance already committed durably —
	// the intake must still report success so the channel is ACKed
	// (best-effort delivery, ADR 0022).
	if advanced, err := app.advance(context.Background(), notif); err != nil || !advanced {
		t.Fatalf("advance = (%v, %v), want (true, nil) despite degraded publish", advanced, err)
	}
}
