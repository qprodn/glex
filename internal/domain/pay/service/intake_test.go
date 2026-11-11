package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
)

// Channel Notification intake, through Accept (ADR 0027). A script gateway
// stands in for the channel, payment and refund modules are the
// collaborators they already are, and the notify log and the publisher
// are in-memory adapters.

const intakeAck = "ACK-OK"

func TestAcceptRejectsCrossMerchantTarget(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 2, Channel: payentity.ChannelWechat}}
	refunds := &intakeRefunds{row: &payentity.Refund{ID: 901, MerchantID: 2, Channel: payentity.ChannelWechat}}
	pub := &memPublisher{}
	in, _, _ := newIntakeUnderTest(payments, refunds, pub, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-pay",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})

	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("payment accept = (%+v, %v), want processed=false ack", res, err)
	}
	if payments.applied != 0 {
		t.Fatal("cross-merchant payment notification advanced the target")
	}

	in, _, _ = newIntakeUnderTest(payments, refunds, pub, payentity.ChannelNotification{
		Kind:        payentity.NotifyKindRefund,
		Channel:     payentity.ChannelWechat,
		NotifyID:    "n-refund",
		TargetNo:    "901",
		RefundState: payentity.RefundSuccess,
	})
	res, err = in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("refund accept = (%+v, %v), want processed=false ack", res, err)
	}
	if refunds.applied != 0 {
		t.Fatal("cross-merchant refund notification advanced the target")
	}
	if len(pub.sent) != 0 {
		t.Fatalf("cross-merchant accept published %d events", len(pub.sent))
	}
}

func TestAcceptRejectsCrossChannelTarget(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelAlipay}}
	pub := &memPublisher{}
	in, _, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-1",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed=false ack", res, err)
	}
	if payments.applied != 0 {
		t.Fatal("cross-channel notification advanced the target")
	}
}

func TestAcceptLandsOwnedPayment(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	in, logs, _ := newIntakeUnderTest(payments, &intakeRefunds{}, &memPublisher{}, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-1",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack", res, err)
	}
	if payments.applied != 1 {
		t.Fatalf("applied = %d, want 1", payments.applied)
	}
	if len(logs.rows) != 1 || !logs.rows[0].Verified || logs.rows[0].RawBody != "raw-body" {
		t.Fatalf("evidence = %+v", logs.rows)
	}
	if len(logs.marked) != 1 || logs.marked[0] != logs.rows[0].ID {
		t.Fatalf("marked = %v, want the evidence id", logs.marked)
	}
}

func TestAcceptAbsorbsUnmatchableTarget(t *testing.T) {
	payments := &intakePayments{getErr: pay.ErrPaymentNotFound}
	refunds := &intakeRefunds{getErr: pay.ErrRefundNotFound}
	in, _, _ := newIntakeUnderTest(payments, refunds, &memPublisher{}, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-pay",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("payment accept = (%+v, %v), want absorbed ack", res, err)
	}

	in, _, _ = newIntakeUnderTest(payments, refunds, &memPublisher{}, payentity.ChannelNotification{
		Kind:        payentity.NotifyKindRefund,
		Channel:     payentity.ChannelWechat,
		NotifyID:    "n-refund",
		TargetNo:    "900",
		RefundState: payentity.RefundSuccess,
	})
	res, err = in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("refund accept = (%+v, %v), want absorbed ack", res, err)
	}
}

func TestAcceptPublishesPaymentCompleted(t *testing.T) {
	paidAt := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	payments := &intakePayments{row: &payentity.Payment{
		ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat,
		BizNo: "ORDER-42", Amount: 100, Currency: "CNY",
	}}
	pub := &memPublisher{}
	in, _, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
		Kind:           payentity.NotifyKindPayment,
		Channel:        payentity.ChannelWechat,
		NotifyID:       "n-1",
		TargetNo:       "900",
		PaymentState:   payentity.PaymentPaid,
		ChannelTradeNo: "wx-trade-1",
		PayerID:        "openid-x",
		PaidAt:         paidAt,
		Amount:         100,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || !res.Processed {
		t.Fatalf("accept = (%+v, %v), want processed", res, err)
	}
	if len(pub.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.sent))
	}
	var got payentity.PaymentCompletedEvent
	if err := json.Unmarshal(pub.sent[0], &got); err != nil {
		t.Fatalf("published payload is not the event JSON: %v", err)
	}
	want := payentity.PaymentCompletedEvent{
		Type:           payentity.EventTypePaymentCompleted,
		EventID:        2, // evidence row takes the first id; the event takes the next
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
	got.OccurredAt = want.OccurredAt
	if got != want {
		t.Fatalf("published event = %+v, want %+v", got, want)
	}
}

func TestAcceptAbsorbedDuplicateDoesNotRepublish(t *testing.T) {
	payments := &intakePayments{
		row:    &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat},
		absorb: true,
	}
	pub := &memPublisher{}
	in, _, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-1",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed=false ack", res, err)
	}
	if len(pub.sent) != 0 {
		t.Fatalf("absorbed duplicate published %d events, want 0", len(pub.sent))
	}
}

func TestAcceptNonPaidTransitionDoesNotPublish(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	pub := &memPublisher{}
	in, _, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-1",
		TargetNo:     "900",
		PaymentState: payentity.PaymentClosed,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack", res, err)
	}
	if len(pub.sent) != 0 {
		t.Fatalf("non-paid transition published %d events, want 0", len(pub.sent))
	}
}

func TestAcceptRedeliveryMarksTheKeptEvidenceRow(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	pub := &memPublisher{}
	in, logs, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-1",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})
	if _, err := in.Accept(context.Background(), intakeRequest()); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	payments.absorb = true
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("redelivery = (%+v, %v), want processed=false ack", res, err)
	}
	if len(logs.rows) != 1 {
		t.Fatalf("evidence rows = %d, want the original one", len(logs.rows))
	}
	kept := logs.rows[0].ID
	if len(logs.marked) != 2 || logs.marked[0] != kept || logs.marked[1] != kept {
		t.Fatalf("marked = %v, want the kept row %d twice", logs.marked, kept)
	}
	if len(pub.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.sent))
	}
}

func TestAcceptFirstPaidRecordsNoticeAndNamesApplication(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	paidAt := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	in := intakeForPayment(fix, paidNotification(p.ID, "n-1", "wx-trade-1", paidAt))
	before := time.Now()
	res, err := in.Accept(context.Background(), intakeForMerchant(fix.merchantID))
	after := time.Now()
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack", res, err)
	}
	if len(fix.completed.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(fix.completed.sent))
	}
	var got payentity.PaymentCompletedEvent
	if err := json.Unmarshal(fix.completed.sent[0], &got); err != nil {
		t.Fatalf("published payload is not the event JSON: %v", err)
	}
	if got.Type != payentity.EventTypePaymentCompleted || got.PaymentID != p.ID || got.ApplicationID != fix.applicationID || got.BizNo != "ORDER-1" {
		t.Fatalf("published event = %+v, want payment %d application %d biz ORDER-1", got, p.ID, fix.applicationID)
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
	notice := fix.notices.rows[0]
	if notice.Kind != payentity.NoticeKindPayment || notice.Status != payentity.NoticeWaiting ||
		notice.ApplicationID != fix.applicationID || notice.PaymentID != p.ID || notice.RefundID != 0 ||
		notice.BizNo != "ORDER-1" || notice.NotifyURL != "https://shop.example/paid" ||
		notice.AttemptCount != 0 || notice.NextAttemptAt.Before(before) || notice.NextAttemptAt.After(after) {
		t.Fatalf("notice = %+v, want one waiting payment notice for ORDER-1", notice)
	}
}

func TestAcceptRepeatPaidDoesNotRepeatNoticeOrEvent(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	in := intakeForPayment(fix, paidNotification(p.ID, "n-1", "wx-trade-1", time.Time{}))
	req := intakeForMerchant(fix.merchantID)
	if _, err := in.Accept(context.Background(), req); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	res, err := in.Accept(context.Background(), req)
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("redelivery = (%+v, %v), want processed=false ack", res, err)
	}
	if len(fix.completed.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 {
		t.Fatalf("notices = %d, want 1", len(fix.notices.rows))
	}
}

// The notice uses the address copied onto the Payment. An empty copy stays
// empty: this path does not read the Pay Application, so filling that
// address later does not create a notice for this Payment.
func TestAcceptPaidWithEmptyCopiedAddressPublishesWithoutNotice(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{})
	fix.apps.m[fix.applicationID].OrderNotifyURL = ""
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	if p.NotifyURL != "" {
		t.Fatalf("copied address = %q, want empty", p.NotifyURL)
	}
	fix.apps.m[fix.applicationID].OrderNotifyURL = "https://shop.example/later"
	in := intakeForPayment(fix, paidNotification(p.ID, "n-1", "wx-trade-1", time.Time{}))
	res, err := in.Accept(context.Background(), intakeForMerchant(fix.merchantID))
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack", res, err)
	}
	if len(fix.completed.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(fix.completed.sent))
	}
	var got payentity.PaymentCompletedEvent
	if err := json.Unmarshal(fix.completed.sent[0], &got); err != nil {
		t.Fatalf("published payload is not the event JSON: %v", err)
	}
	if got.ApplicationID != fix.applicationID {
		t.Fatalf("published event = %+v, want application %d", got, fix.applicationID)
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %+v, want none when the copied address is empty", fix.notices.rows)
	}
}

func TestAcceptNonPaidIntakeRecordsNoNotice(t *testing.T) {
	states := []payentity.PaymentStatus{
		payentity.PaymentFailed,
		payentity.PaymentClosed,
		payentity.PaymentExpired,
	}
	for _, state := range states {
		payments := &intakePayments{row: &payentity.Payment{
			ID: 900, ApplicationID: 42, MerchantID: 7, Channel: payentity.ChannelWechat,
			BizNo: "ORDER-42", NotifyURL: "https://caller.example/paid",
		}}
		pub := &memPublisher{}
		in, _, notices := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
			Kind:         payentity.NotifyKindPayment,
			Channel:      payentity.ChannelWechat,
			NotifyID:     "n-1",
			TargetNo:     "900",
			PaymentState: state,
		})
		res, err := in.Accept(context.Background(), intakeRequest())
		if err != nil || !res.Processed || res.AckBody != intakeAck {
			t.Fatalf("state %d accept = (%+v, %v), want processed ack", state, res, err)
		}
		if len(pub.sent) != 0 {
			t.Fatalf("state %d published %d events, want 0", state, len(pub.sent))
		}
		if len(notices.rows) != 0 {
			t.Fatalf("state %d notices = %d, want 0", state, len(notices.rows))
		}
	}
}

func TestAcceptExpiredPendingPublishesWhenItsCasLandsPaid(t *testing.T) {
	gw := &scriptGateway{queryState: paidChannelState("wx-from-query")}
	fix := newPayFixture(t, gw)
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.payments.p[p.ID].ExpireTime = time.Now().Add(-time.Hour)
	paidAt := time.Date(2026, 9, 14, 16, 0, 0, 0, time.UTC)
	notif := payentity.ChannelNotification{
		Kind:           payentity.NotifyKindPayment,
		Channel:        payentity.ChannelWechat,
		NotifyID:       "n-expired",
		TargetNo:       strconv.FormatInt(p.ID, 10),
		PaymentState:   payentity.PaymentPaid,
		ChannelTradeNo: "wx-from-notify",
		PayerID:        "openid-notify",
		PaidAt:         paidAt,
		Amount:         100,
	}
	in := NewIntake(
		&intakeMerchants{gw: &intakeGateway{ack: intakeAck, notif: notif}},
		fix.svc,
		&intakeRefunds{},
		&memNotifyLog{},
		fix.completed,
		slog.New(slog.DiscardHandler),
	)
	req := &ChannelNotificationRequest{
		Channel:    payentity.ChannelWechat,
		MerchantID: fix.merchantID,
		Headers:    http.Header{"Wechatpay-Signature": []string{"sig"}},
		Body:       []byte("raw-body"),
	}
	res, err := in.Accept(context.Background(), req)
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack", res, err)
	}
	stored := fix.payments.p[p.ID]
	if stored.Status != payentity.PaymentPaid || stored.ChannelTradeNo != "wx-from-notify" {
		t.Fatalf("payment = %+v, want paid by the notification's compare-and-swap", stored)
	}
	if len(fix.completed.sent) != 1 {
		t.Fatalf("published %d events, want 1", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 1 || fix.notices.rows[0].PaymentID != p.ID || fix.notices.rows[0].NotifyURL != "https://shop.example/paid" {
		t.Fatalf("notices = %+v, want the one due row for the paid payment", fix.notices.rows)
	}
	res, err = in.Accept(context.Background(), req)
	if err != nil || res.Processed || res.AckBody != intakeAck {
		t.Fatalf("repeat = (%+v, %v), want processed=false ack", res, err)
	}
	if len(fix.completed.sent) != 1 || len(fix.notices.rows) != 1 {
		t.Fatalf("repeat published %d events and left %d notices, want neither again", len(fix.completed.sent), len(fix.notices.rows))
	}
}

func TestAcceptPaidNoticeInsertFailureDoesNotAck(t *testing.T) {
	fix := newPayFixture(t, &scriptGateway{})
	p, err := fix.svc.Initiate(context.Background(), initiateInput(fix.applicationID, 7))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	fix.notices.err = errors.New("store down")
	in := intakeForPayment(fix, paidNotification(p.ID, "n-1", "wx-trade-1", time.Time{}))
	if _, err := in.Accept(context.Background(), intakeForMerchant(fix.merchantID)); err == nil {
		t.Fatal("accept succeeded, want no ack when the notice cannot commit with the payment")
	}
	stored := fix.payments.p[p.ID]
	if stored.Status != payentity.PaymentPending {
		t.Fatalf("payment = %+v, want still pending", stored)
	}
	if len(fix.completed.sent) != 0 {
		t.Fatalf("published %d events, want 0", len(fix.completed.sent))
	}
	if len(fix.notices.rows) != 0 {
		t.Fatalf("notices = %d, want no due row", len(fix.notices.rows))
	}
}

func TestAcceptRefundRecordsNoBusinessNotice(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{
		ID: 900, ApplicationID: 42, MerchantID: 7, Channel: payentity.ChannelWechat,
		NotifyURL: "https://caller.example/paid",
	}}
	refunds := &intakeRefunds{row: &payentity.Refund{ID: 901, MerchantID: 7, Channel: payentity.ChannelWechat}}
	pub := &memPublisher{}
	in, _, notices := newIntakeUnderTest(payments, refunds, pub, payentity.ChannelNotification{
		Kind:        payentity.NotifyKindRefund,
		Channel:     payentity.ChannelWechat,
		NotifyID:    "n-refund",
		TargetNo:    "901",
		RefundState: payentity.RefundSuccess,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack", res, err)
	}
	if len(pub.sent) != 0 || len(notices.rows) != 0 {
		t.Fatalf("refund intake published %d events and recorded %d notices, want neither", len(pub.sent), len(notices.rows))
	}
}

func TestAcceptPublishDegradationStillAcks(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	pub := &memPublisher{err: errors.New("queue full")}
	in, _, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
		Kind:         payentity.NotifyKindPayment,
		Channel:      payentity.ChannelWechat,
		NotifyID:     "n-1",
		TargetNo:     "900",
		PaymentState: payentity.PaymentPaid,
	})
	res, err := in.Accept(context.Background(), intakeRequest())
	if err != nil || !res.Processed || res.AckBody != intakeAck {
		t.Fatalf("accept = (%+v, %v), want processed ack despite degraded publish", res, err)
	}
}

func paidNotification(paymentID int64, notifyID, tradeNo string, paidAt time.Time) payentity.ChannelNotification {
	return payentity.ChannelNotification{
		Kind:           payentity.NotifyKindPayment,
		Channel:        payentity.ChannelWechat,
		NotifyID:       notifyID,
		TargetNo:       strconv.FormatInt(paymentID, 10),
		PaymentState:   payentity.PaymentPaid,
		ChannelTradeNo: tradeNo,
		PayerID:        "openid-x",
		PaidAt:         paidAt,
		Amount:         100,
	}
}

func intakeForPayment(fix payFixture, notif payentity.ChannelNotification) Intake {
	return NewIntake(
		&intakeMerchants{gw: &intakeGateway{ack: intakeAck, notif: notif}},
		fix.svc,
		&intakeRefunds{},
		&memNotifyLog{},
		fix.completed,
		slog.New(slog.DiscardHandler),
	)
}

func intakeForMerchant(merchantID int64) *ChannelNotificationRequest {
	return &ChannelNotificationRequest{
		Channel:    payentity.ChannelWechat,
		MerchantID: merchantID,
		Headers:    http.Header{"Wechatpay-Signature": []string{"sig"}},
		Body:       []byte("raw-body"),
	}
}

func intakeRequest() *ChannelNotificationRequest {
	return &ChannelNotificationRequest{
		Channel:    payentity.ChannelWechat,
		MerchantID: 7,
		Headers:    http.Header{"Wechatpay-Signature": []string{"sig"}},
		Body:       []byte("raw-body"),
	}
}

func newIntakeUnderTest(payments Payment, refunds Refund, pub CompletedPublisher, notif payentity.ChannelNotification) (Intake, *memNotifyLog, *memNotices) {
	logs := &memNotifyLog{}
	notices := &memNotices{}
	gw := &intakeGateway{ack: intakeAck, notif: notif}
	in := NewIntake(&intakeMerchants{gw: gw}, payments, refunds, logs, pub, slog.New(slog.DiscardHandler))
	return in, logs, notices
}

type intakeGateway struct {
	ack   string
	notif payentity.ChannelNotification
}

func (g *intakeGateway) Channel() payentity.Channel { return payentity.ChannelWechat }
func (g *intakeGateway) Prepay(context.Context, PrepayRequest) (PrepayResult, error) {
	return PrepayResult{}, nil
}
func (g *intakeGateway) Query(context.Context, QueryRequest) (ChannelPaymentState, error) {
	return ChannelPaymentState{}, nil
}
func (g *intakeGateway) Close(context.Context, CloseRequest) error { return nil }
func (g *intakeGateway) Refund(context.Context, RefundRequest) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}
func (g *intakeGateway) QueryRefund(context.Context, RefundQueryRequest) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}
func (g *intakeGateway) ParseNotification(context.Context, NotificationRequest) (payentity.ChannelNotification, error) {
	return g.notif, nil
}
func (g *intakeGateway) Probe(context.Context, *payentity.MerchantAccount) error { return nil }
func (g *intakeGateway) AckSuccess() string                                      { return g.ack }

type intakeMerchants struct {
	Merchant
	gw ChannelGateway
}

func (m *intakeMerchants) ResolveGateway(context.Context, int64) (*ResolvedGateway, error) {
	return &ResolvedGateway{Gateway: m.gw, Merchant: &payentity.MerchantAccount{ID: 7}}, nil
}

type intakePayments struct {
	Payment
	row     *payentity.Payment
	getErr  error
	applied int
	absorb  bool
}

func (p *intakePayments) Get(context.Context, int64) (*payentity.Payment, error) {
	if p.getErr != nil {
		return nil, p.getErr
	}
	return p.row, nil
}

func (p *intakePayments) Load(ctx context.Context, id int64) (*payentity.Payment, error) {
	return p.Get(ctx, id)
}

func (p *intakePayments) ApplyChannelState(context.Context, ApplyChannelStateRequest) (bool, error) {
	p.applied++
	if p.absorb {
		return false, nil
	}
	return true, nil
}

type intakeRefunds struct {
	Refund
	row     *payentity.Refund
	getErr  error
	applied int
}

func (r *intakeRefunds) Get(context.Context, int64) (*payentity.Refund, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.row, nil
}

func (r *intakeRefunds) ApplyRefundState(context.Context, ApplyRefundStateRequest) (bool, error) {
	r.applied++
	return true, nil
}

type memNotifyLog struct {
	next   int64
	rows   []*payentity.ChannelNotification
	marked []int64
}

func (m *memNotifyLog) NextID() int64 {
	m.next++
	return m.next
}

func (m *memNotifyLog) Insert(_ context.Context, n *payentity.ChannelNotification) (bool, error) {
	for _, row := range m.rows {
		if row.Channel == n.Channel && row.NotifyID != "" && row.NotifyID == n.NotifyID {
			n.ID = row.ID
			return false, nil
		}
	}
	cp := *n
	m.rows = append(m.rows, &cp)
	return true, nil
}

func (m *memNotifyLog) MarkProcessed(_ context.Context, id int64) error {
	m.marked = append(m.marked, id)
	return nil
}

var _ payrepo.NotifyLogRepo = (*memNotifyLog)(nil)

type memNotices struct {
	next    int64
	rows    []*payentity.BusinessNotice
	inserts int
	err     error
	// refuse is the unique-pair answer: ok false, no error, no row.
	refuse bool
}

func (m *memNotices) NextID() int64 {
	m.next++
	return m.next
}

func (m *memNotices) Insert(_ context.Context, n *payentity.BusinessNotice) (bool, error) {
	m.inserts++
	if m.err != nil {
		return false, m.err
	}
	if m.refuse {
		return false, nil
	}
	for _, row := range m.rows {
		if row.Kind == n.Kind && n.Kind == payentity.NoticeKindPayment && n.PaymentID != 0 && row.PaymentID == n.PaymentID {
			return false, nil
		}
		if row.Kind == n.Kind && n.Kind == payentity.NoticeKindRefund && n.RefundID != 0 && row.RefundID == n.RefundID {
			return false, nil
		}
	}
	cp := *n
	m.rows = append(m.rows, &cp)
	return true, nil
}

func (m *memNotices) ListDue(context.Context, payrepo.ListDueNoticesRequest) ([]*payentity.BusinessNotice, error) {
	return nil, nil
}

func (m *memNotices) ClaimDueNotice(context.Context, payrepo.ClaimDueNoticeRequest) (bool, error) {
	return false, nil
}

func (m *memNotices) FindByID(context.Context, int64) (*payentity.BusinessNotice, error) {
	return nil, pay.ErrBusinessNoticeNotFound
}

func (m *memNotices) RecordManualAttempt(context.Context, payrepo.RecordManualAttemptRequest) error {
	return nil
}

func (m *memNotices) ListExhausted(context.Context, listq.Query) ([]*payrepo.ExhaustedBusinessNotice, error) {
	return nil, nil
}

func (m *memNotices) RecordAutomaticAttempt(context.Context, payrepo.RecordAutomaticAttemptRequest) error {
	return nil
}

var _ payrepo.BusinessNoticeRepo = (*memNotices)(nil)

type memPublisher struct {
	sent [][]byte
	err  error
}

func (p *memPublisher) Publish(_ context.Context, body []byte) error {
	if p.err != nil {
		return p.err
	}
	p.sent = append(p.sent, append([]byte(nil), body...))
	return nil
}
