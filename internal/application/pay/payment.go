package pay

import (
	"context"
	"fmt"

	payv1 "glex/api/pay/v1"
	"glex/internal/bizpkg"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertPayment maps a payentity.Payment DO to the proto DTO.
func convertPayment(p *payentity.Payment) *payv1.Payment {
	out := &payv1.Payment{
		PaymentId:      p.ID,
		MerchantId:     p.MerchantID,
		Channel:        int32(p.Channel),
		BizNo:          p.BizNo,
		Subject:        p.Subject,
		Amount:         p.Amount,
		Currency:       p.Currency,
		PayMethod:      int32(p.PayMethod),
		Status:         int32(p.Status),
		ChannelTradeNo: p.ChannelTradeNo,
		ChannelPayload: p.ChannelPayload,
		PayerId:        p.PayerID,
		RefundedAmount: p.RefundedAmount,
		FailReason:     p.FailReason,
		ExpireTime:     timestamppb.New(p.ExpireTime),
		CreateBy:       p.CreateBy,
		UpdateBy:       p.UpdateBy,
		CreatedAt:      timestamppb.New(p.CreatedAt),
		UpdatedAt:      timestamppb.New(p.UpdatedAt),
	}
	if p.PaidAt != nil {
		out.PaidAt = timestamppb.New(*p.PaidAt)
	}
	return out
}

// CreatePayment initiates one Native payment: the prepay lock absorbs
// concurrent duplicate submits (LOCK_BUSY / HTTP 409), the close-old +
// insert batch runs in one transaction, and the channel prepay — which
// must never ride inside that transaction — follows before the response.
func (s *PayApplication) CreatePayment(ctx context.Context, req *payv1.CreatePaymentRequest) (*payv1.Payment, error) {
	in := payservice.CreatePaymentRequest{
		MerchantID:    req.GetMerchantId(),
		BizNo:         req.GetBizNo(),
		Subject:       req.GetSubject(),
		Amount:        req.GetAmount(),
		PayMethod:     payentity.PayMethod(req.GetPayMethod()),
		ExpireMinutes: req.GetExpireMinutes(),
		ActorID:       s.actor(ctx),
	}
	if in.PayMethod == 0 {
		// Native is the MVP default (design §4).
		in.PayMethod = payentity.PayMethodNative
	}
	var out *payentity.Payment
	err := s.locker.WithLock(ctx, fmt.Sprintf("pay:prepay:%d:%s", in.MerchantID, in.BizNo), func(ctx context.Context) error {
		if err := s.tx.InTx(ctx, func(ctx context.Context) error {
			var err error
			out, err = s.payments.Initiate(ctx, in)
			return err
		}); err != nil {
			return err
		}
		var err error
		out, err = s.payments.Prepay(ctx, payservice.PrepayPaymentRequest{
			ID:            out.ID,
			NotifyBaseURL: s.payConfig.GetNotifyBaseUrl(),
		})
		return err
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.create_payment", err)
		return nil, err
	}
	return convertPayment(out), nil
}

// GetPayment retrieves one Payment; a PENDING payment past its expiry
// settles on the read (lazy expiry).
func (s *PayApplication) GetPayment(ctx context.Context, req *payv1.GetPaymentRequest) (*payv1.Payment, error) {
	p, err := s.payments.Get(ctx, req.GetPaymentId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.get_payment", err)
		return nil, err
	}
	return convertPayment(p), nil
}

// paymentListSpec is this module's list fact (ADR 0024): filterable
// idents plus the default listing order.
var paymentListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("biz_no", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("merchant_id", filtering.TypeInt),
		filtering.DeclareIdent("channel", filtering.TypeInt),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListPayments lists Payments with AIP filter/order/page-token. List
// reads never touch the Channel (design §5).
func (s *PayApplication) ListPayments(ctx context.Context, req *payv1.ListPaymentsRequest) (*payv1.PaymentSet, error) {
	q, page, err := listq.Parse(req, paymentListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_payments", err)
		return nil, err
	}
	payments, err := s.payments.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_payments", err)
		return nil, err
	}
	set := &payv1.PaymentSet{Payments: make([]*payv1.Payment, 0, len(payments))}
	for _, p := range payments {
		set.Payments = append(set.Payments, convertPayment(p))
	}
	set.NextPageToken = page.NextToken(len(payments))
	return set, nil
}

// QueryPayment asks the Channel for the payment's true state and advances
// it — the explicit settlement path when callbacks are lost.
func (s *PayApplication) QueryPayment(ctx context.Context, req *payv1.QueryPaymentRequest) (*payv1.Payment, error) {
	p, err := s.payments.Query(ctx, req.GetPaymentId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.query_payment", err)
		return nil, err
	}
	return convertPayment(p), nil
}

// ClosePayment manually closes a PENDING payment at the channel, then
// locally; a channel "already paid" flips into a query and settles PAID.
func (s *PayApplication) ClosePayment(ctx context.Context, req *payv1.ClosePaymentRequest) (*payv1.Payment, error) {
	p, err := s.payments.Close(ctx, payservice.ClosePaymentRequest{
		ID:      req.GetPaymentId(),
		ActorID: bizpkg.AuditActor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.close_payment", err)
		return nil, err
	}
	return convertPayment(p), nil
}
