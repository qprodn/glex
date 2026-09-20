package impl

import (
	"context"
	"log/slog"
	"strconv"

	payv1 "glex/api/pay/v1"
	"glex/internal/application/pay"
	paycontract "glex/internal/crossdomain/pay"
	"glex/internal/crossdomain/pay/model"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
)

// paymentCreator satisfies the crossdomain creation contract by sharing
// the back office's application use case — the prepay lock, the
// close-old-pending transaction batch, and the channel prepay all live
// there, so an in-process caller gets exactly the initiation the console
// gets (design §8).
type paymentCreator struct {
	app *pay.PayApplication
}

// NewPaymentCreator adapts the pay application to the crossdomain
// creation contract.
func NewPaymentCreator(app *pay.PayApplication) paycontract.PaymentCreator {
	return &paymentCreator{app: app}
}

func (c *paymentCreator) CreatePayment(ctx context.Context, req *model.PaymentRequest) (*model.Payment, error) {
	out, err := c.app.CreatePayment(ctx, &payv1.CreatePaymentRequest{
		MerchantId:    req.MerchantID,
		BizNo:         req.BizNo,
		Subject:       req.Subject,
		Amount:        req.Amount,
		PayMethod:     int32(req.PayMethod),
		ExpireMinutes: req.ExpireMinutes,
	})
	if err != nil {
		return nil, err
	}
	return &model.Payment{
		PaymentNo:      out.GetPaymentId(),
		MerchantID:     out.GetMerchantId(),
		Channel:        int(out.GetChannel()),
		BizNo:          out.GetBizNo(),
		Subject:        out.GetSubject(),
		Amount:         out.GetAmount(),
		Currency:       out.GetCurrency(),
		PayMethod:      int(out.GetPayMethod()),
		Status:         int(out.GetStatus()),
		ChannelTradeNo: out.GetChannelTradeNo(),
		ChannelPayload: out.GetChannelPayload(),
		PayerID:        out.GetPayerId(),
		RefundedAmount: out.GetRefundedAmount(),
		FailReason:     out.GetFailReason(),
	}, nil
}

// paymentReader satisfies the crossdomain read contract over the payment
// domain service.
type paymentReader struct {
	payments payservicePayment
	logger   *slog.Logger
}

// payservicePayment narrows the domain service to the reads the reader
// contract needs — the interface the impl depends on.
type payservicePayment interface {
	Get(ctx context.Context, id int64) (*payentity.Payment, error)
	GetByBizNo(ctx context.Context, req *payservice.GetByBizNoRequest) (*payentity.Payment, error)
}

// NewPaymentReader adapts the payment domain service to the crossdomain
// read contract.
func NewPaymentReader(payments payservicePayment, logger *slog.Logger) paycontract.PaymentReader {
	return &paymentReader{
		payments: payments,
		logger:   logger.With(slog.String("component", "crossdomain.pay")),
	}
}

func (r *paymentReader) GetPayment(ctx context.Context, paymentNo string) (*model.Payment, error) {
	id, err := strconv.ParseInt(paymentNo, 10, 64)
	if err != nil {
		return nil, err
	}
	p, err := r.payments.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return paymentToModel(p), nil
}

func (r *paymentReader) GetByBizNo(ctx context.Context, req *model.GetByBizNoRequest) (*model.Payment, error) {
	p, err := r.payments.GetByBizNo(ctx, &payservice.GetByBizNoRequest{
		MerchantID: req.MerchantID,
		BizNo:      req.BizNo,
	})
	if err != nil {
		return nil, err
	}
	return paymentToModel(p), nil
}

// paymentToModel maps the domain entity to the contract DTO.
func paymentToModel(p *payentity.Payment) *model.Payment {
	out := &model.Payment{
		PaymentNo:      p.ID,
		MerchantID:     p.MerchantID,
		Channel:        int(p.Channel),
		BizNo:          p.BizNo,
		Subject:        p.Subject,
		Amount:         p.Amount,
		Currency:       p.Currency,
		PayMethod:      int(p.PayMethod),
		Status:         int(p.Status),
		ChannelTradeNo: p.ChannelTradeNo,
		ChannelPayload: p.ChannelPayload,
		PayerID:        p.PayerID,
		RefundedAmount: p.RefundedAmount,
		FailReason:     p.FailReason,
		ExpireTime:     p.ExpireTime,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
	if p.PaidAt != nil {
		out.PaidAt = p.PaidAt
	}
	return out
}
