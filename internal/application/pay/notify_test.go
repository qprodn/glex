package pay

import (
	"context"
	"log/slog"
	"testing"

	"glex/internal/conf"
	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
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
}

func (f *advancePaymentSvc) Get(ctx context.Context, id int64) (*payentity.Payment, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.row, nil
}

func (f *advancePaymentSvc) ApplyChannelState(ctx context.Context, paymentNo int64, state payservice.ChannelPaymentState) (bool, error) {
	f.applied++
	return true, nil
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

func (f *advanceRefundSvc) ApplyRefundState(ctx context.Context, refundNo int64, state payservice.ChannelRefundState) (bool, error) {
	f.applied++
	return true, nil
}

func newAdvanceTestApp(payments payservice.Payment, refunds payservice.Refund) *PayApplication {
	return NewPayApplication(
		&fakeMerchantSvc{},
		payments,
		refunds,
		&fakeNotificationSvc{},
		&fakeTx{},
		&fakeLocker{},
		&conf.Pay{},
		slog.New(slog.DiscardHandler),
	)
}

func TestAdvanceRejectsCrossMerchantTarget(t *testing.T) {
	payments := &advancePaymentSvc{row: &payentity.Payment{ID: 900, MerchantID: 2, Channel: payentity.ChannelWechat}}
	refunds := &advanceRefundSvc{row: &payentity.Refund{ID: 901, MerchantID: 2, Channel: payentity.ChannelWechat}}
	app := newAdvanceTestApp(payments, refunds)

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
	app := newAdvanceTestApp(payments, &advanceRefundSvc{})
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
	app := newAdvanceTestApp(payments, &advanceRefundSvc{})
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
	app := newAdvanceTestApp(payments, refunds)

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
