package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
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
	in, _ := newIntakeUnderTest(payments, refunds, pub, payentity.ChannelNotification{
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

	in, _ = newIntakeUnderTest(payments, refunds, pub, payentity.ChannelNotification{
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
	in, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
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
	in, logs := newIntakeUnderTest(payments, &intakeRefunds{}, &memPublisher{}, payentity.ChannelNotification{
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
	in, _ := newIntakeUnderTest(payments, refunds, &memPublisher{}, payentity.ChannelNotification{
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

	in, _ = newIntakeUnderTest(payments, refunds, &memPublisher{}, payentity.ChannelNotification{
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
	in, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
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
	in, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
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
	in, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
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
	in, logs := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
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

func TestAcceptPublishDegradationStillAcks(t *testing.T) {
	payments := &intakePayments{row: &payentity.Payment{ID: 900, MerchantID: 7, Channel: payentity.ChannelWechat}}
	pub := &memPublisher{err: errors.New("queue full")}
	in, _ := newIntakeUnderTest(payments, &intakeRefunds{}, pub, payentity.ChannelNotification{
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

func intakeRequest() *ChannelNotificationRequest {
	return &ChannelNotificationRequest{
		Channel:    payentity.ChannelWechat,
		MerchantID: 7,
		Headers:    http.Header{"Wechatpay-Signature": []string{"sig"}},
		Body:       []byte("raw-body"),
	}
}

func newIntakeUnderTest(payments Payment, refunds Refund, pub CompletedPublisher, notif payentity.ChannelNotification) (Intake, *memNotifyLog) {
	logs := &memNotifyLog{}
	gw := &intakeGateway{ack: intakeAck, notif: notif}
	in := NewIntake(&intakeMerchants{gw: gw}, payments, refunds, logs, pub, slog.New(slog.DiscardHandler))
	return in, logs
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
