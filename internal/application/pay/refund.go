package pay

import (
	"context"
	"fmt"

	payv1 "glex/api/pay/v1"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertRefund maps a payentity.Refund DO to the proto DTO.
func convertRefund(r *payentity.Refund) *payv1.Refund {
	out := &payv1.Refund{
		RefundId:        r.ID,
		PaymentId:       r.PaymentID,
		MerchantId:      r.MerchantID,
		Channel:         int32(r.Channel),
		Amount:          r.Amount,
		Reason:          r.Reason,
		Status:          int32(r.Status),
		ChannelRefundNo: r.ChannelRefundNo,
		FailReason:      r.FailReason,
		CreateBy:        r.CreateBy,
		UpdateBy:        r.UpdateBy,
		CreatedAt:       timestamppb.New(r.CreatedAt),
		UpdatedAt:       timestamppb.New(r.UpdatedAt),
	}
	if r.RefundedAt != nil {
		out.RefundedAt = timestamppb.New(*r.RefundedAt)
	}
	return out
}

// CreateRefund refunds part of a paid Payment under the payment-scoped
// lock — the cap (refunded + this ≤ paid) is checked inside it, so
// concurrent refunds of one payment serialize instead of racing past the
// sum. WeChat refunds return PENDING (the callback settles); Alipay's
// synchronous answer settles immediately.
func (s *PayApplication) CreateRefund(ctx context.Context, req *payv1.CreateRefundRequest) (*payv1.Refund, error) {
	in := &payservice.CreateRefundInput{
		PaymentID: req.GetPaymentId(),
		Amount:    req.GetAmount(),
		Reason:    req.GetReason(),
	}
	var out *payentity.Refund
	err := s.locker.WithLock(ctx, fmt.Sprintf("pay:refund:%d", in.PaymentID), func(ctx context.Context) error {
		created, err := s.refunds.Create(ctx, in, s.actor(ctx))
		if err != nil {
			return err
		}
		out, err = s.refunds.PrepayRefund(ctx, created.ID, s.payConfig.GetNotifyBaseUrl())
		return err
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.create_refund", err)
		return nil, err
	}
	return convertRefund(out), nil
}

// GetRefund retrieves one Refund.
func (s *PayApplication) GetRefund(ctx context.Context, req *payv1.GetRefundRequest) (*payv1.Refund, error) {
	rf, err := s.refunds.Get(ctx, req.GetRefundId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.get_refund", err)
		return nil, err
	}
	return convertRefund(rf), nil
}

// refundListSpec is this module's list fact (ADR 0024): filterable
// idents plus the default listing order.
var refundListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("payment_id", filtering.TypeInt),
		filtering.DeclareIdent("merchant_id", filtering.TypeInt),
		filtering.DeclareIdent("channel", filtering.TypeInt),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListRefunds lists Refunds with AIP filter/order/page-token.
func (s *PayApplication) ListRefunds(ctx context.Context, req *payv1.ListRefundsRequest) (*payv1.RefundSet, error) {
	q, page, err := listq.Parse(req, refundListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_refunds", err)
		return nil, err
	}
	refunds, err := s.refunds.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_refunds", err)
		return nil, err
	}
	set := &payv1.RefundSet{Refunds: make([]*payv1.Refund, 0, len(refunds))}
	for _, rf := range refunds {
		set.Refunds = append(set.Refunds, convertRefund(rf))
	}
	set.NextPageToken = page.NextToken(len(refunds))
	return set, nil
}

// QueryRefund asks the Channel for the refund's true state and advances it.
func (s *PayApplication) QueryRefund(ctx context.Context, req *payv1.QueryRefundRequest) (*payv1.Refund, error) {
	rf, err := s.refunds.Query(ctx, req.GetRefundId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.query_refund", err)
		return nil, err
	}
	return convertRefund(rf), nil
}
