package pay

import (
	"context"
	"time"

	payv1 "glex/api/pay/v1"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// exhaustedNoticeListSpec is the exhausted-notice queue (ADR 0024). The
// queue is exhausted rows only, so no filter idents are declared. The
// default order is newest id first.
var exhaustedNoticeListSpec = listq.Spec{
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListExhaustedBusinessNotices lists Business Notifications whose automatic
// delivery is exhausted, each with its latest attempt.
func (s *PayApplication) ListExhaustedBusinessNotices(ctx context.Context, req *payv1.ListExhaustedBusinessNoticesRequest) (*payv1.BusinessNoticeSet, error) {
	q, page, err := listq.Parse(req, exhaustedNoticeListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_exhausted_business_notices", err)
		return nil, err
	}
	rows, err := s.notices.ListExhausted(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_exhausted_business_notices", err)
		return nil, err
	}
	set := &payv1.BusinessNoticeSet{Notices: make([]*payv1.BusinessNotice, 0, len(rows))}
	for _, row := range rows {
		set.Notices = append(set.Notices, convertBusinessNotice(row.Notice, row.Last))
	}
	set.NextPageToken = page.NextToken(len(rows))
	return set, nil
}

// ResendBusinessNotice delivers one exhausted notice to the Pay
// Application's current result address of the same kind. A failed HTTP
// call is the attempt's outcome and returns the notice still exhausted.
func (s *PayApplication) ResendBusinessNotice(ctx context.Context, req *payv1.ResendBusinessNoticeRequest) (*payv1.BusinessNotice, error) {
	result, err := s.notices.Resend(ctx, payservice.ResendNoticeRequest{
		ID:      req.GetId(),
		Now:     time.Now(),
		ActorID: s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.resend_business_notice", err)
		return nil, err
	}
	return convertBusinessNotice(result.Notice, result.Attempt), nil
}

func convertBusinessNotice(n *payentity.BusinessNotice, last *payentity.BusinessNoticeAttempt) *payv1.BusinessNotice {
	out := &payv1.BusinessNotice{
		Id:            n.ID,
		ApplicationId: n.ApplicationID,
		Kind:          int32(n.Kind),
		PaymentId:     n.PaymentID,
		RefundId:      n.RefundID,
		BizNo:         n.BizNo,
		NotifyUrl:     n.NotifyURL,
		Status:        int32(n.Status),
		AttemptCount:  int32(n.AttemptCount),
	}
	if !n.NextAttemptAt.IsZero() {
		out.NextAttemptAt = timestamppb.New(n.NextAttemptAt)
	}
	if last == nil {
		return out
	}
	out.HasLastAttempt = true
	out.LastManual = last.Manual
	out.LastError = last.Error
	out.LastAccepted = last.Accepted
	if last.HTTPStatus != nil {
		out.LastHttpStatus = int32(*last.HTTPStatus)
	}
	if !last.CreatedAt.IsZero() {
		out.LastAttemptAt = timestamppb.New(last.CreatedAt)
	}
	return out
}
