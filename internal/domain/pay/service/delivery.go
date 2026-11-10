package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/logx"
)

// noticeDeliverBatch bounds one scan so a backlog waits for the next run.
const noticeDeliverBatch = 50

// automaticAttemptLimit is the number of automatic tries, including the
// first. automaticRetryGaps are the waits after each failed try except
// the last, which exhausts the notice.
const automaticAttemptLimit = 9

var automaticRetryGaps = []time.Duration{
	15 * time.Second,
	15 * time.Second,
	30 * time.Second,
	3 * time.Minute,
	30 * time.Minute,
	30 * time.Minute,
	30 * time.Minute,
	60 * time.Minute,
}

// Delivery posts due Business Notifications. The address is the one stored
// on the notice. A failed attempt is that attempt's outcome: it does not
// fail the caller.
type Delivery interface {
	DeliverDue(ctx context.Context, req DeliverDueRequest) (int, error)
}

// DeliverDueRequest is one scan of notices that are waiting and due.
type DeliverDueRequest struct {
	Now time.Time
}

// NoticePost is one unsigned JSON delivery to a result address.
type NoticePost struct {
	URL  string
	Body []byte
}

// NoticePostResult is the HTTP outcome. StatusCode is zero when the call
// did not produce a response.
type NoticePostResult struct {
	StatusCode int
}

// NoticePoster is the HTTP boundary for automatic delivery. An error is a
// timeout or a connection failure, which is a failed attempt.
type NoticePoster interface {
	Post(ctx context.Context, req NoticePost) (NoticePostResult, error)
}

type deliveryImpl struct {
	notices payrepo.BusinessNoticeRepo
	poster  NoticePoster
	logger  *slog.Logger
}

type paymentNoticeBody struct {
	BizNo     string `json:"biz_no"`
	PaymentID int64  `json:"payment_id"`
}

type refundNoticeBody struct {
	RefundID  int64 `json:"refund_id"`
	PaymentID int64 `json:"payment_id"`
}

// NewDelivery creates the automatic-delivery service.
func NewDelivery(notices payrepo.BusinessNoticeRepo, poster NoticePoster, logger *slog.Logger) Delivery {
	return &deliveryImpl{
		notices: notices,
		poster:  poster,
		logger:  logger.With(slog.String("component", "domain.pay.notice.svc")),
	}
}

// DeliverDue posts every waiting notice that is due at req.Now.
func (s *deliveryImpl) DeliverDue(ctx context.Context, req DeliverDueRequest) (int, error) {
	due, err := s.notices.ListDue(ctx, payrepo.ListDueNoticesRequest{Now: req.Now, Limit: noticeDeliverBatch})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.deliver", err)
		return 0, err
	}
	attempted := 0
	for _, n := range due {
		if err := ctx.Err(); err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.notice.deliver", err)
			return attempted, err
		}
		body, err := noticeBody(n)
		if err != nil {
			s.logger.Error("business notice body marshal failed",
				slog.String("operation", "pay.svc.notice.deliver"),
				slog.Int64("notice_id", n.ID),
				slog.Any("error", err))
			continue
		}
		ok, err := s.notices.ClaimDueNotice(ctx, payrepo.ClaimDueNoticeRequest{
			ID:    n.ID,
			Now:   req.Now,
			Until: req.Now.Add(automaticRetryGaps[0]),
		})
		if err != nil {
			s.logger.Warn("business notice delivery could not claim the notice",
				slog.String("operation", "pay.svc.notice.deliver"),
				slog.Int64("notice_id", n.ID),
				slog.Any("error", err))
			continue
		}
		if !ok {
			continue
		}
		result, postErr := s.poster.Post(ctx, NoticePost{URL: n.NotifyURL, Body: body})
		if err := ctx.Err(); err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.notice.deliver", err)
			return attempted, err
		}
		attempt := &payentity.BusinessNoticeAttempt{
			ID:        s.notices.NextID(),
			NoticeID:  n.ID,
			CreatedAt: req.Now,
		}
		if postErr != nil {
			attempt.Error = clipAttemptError(postErr.Error())
		} else {
			code := result.StatusCode
			attempt.HTTPStatus = &code
			attempt.Accepted = code >= 200 && code < 300
		}
		n.AttemptCount++
		if attempt.Accepted {
			n.Status = payentity.NoticeAccepted
			n.NextAttemptAt = time.Time{}
		} else if n.AttemptCount >= automaticAttemptLimit {
			n.Status = payentity.NoticeExhausted
			n.NextAttemptAt = time.Time{}
		} else {
			n.Status = payentity.NoticeWaiting
			n.NextAttemptAt = req.Now.Add(automaticRetryGaps[n.AttemptCount-1])
		}
		if err := s.notices.RecordAutomaticAttempt(ctx, payrepo.RecordAutomaticAttemptRequest{
			Notice:  n,
			Attempt: attempt,
		}); err != nil {
			s.logger.Warn("business notice delivery could not record the attempt",
				slog.String("operation", "pay.svc.notice.deliver"),
				slog.Int64("notice_id", n.ID),
				slog.Any("error", err))
			continue
		}
		attempted++
	}
	return attempted, nil
}

func clipAttemptError(s string) string {
	const max = 512
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func noticeBody(n *payentity.BusinessNotice) ([]byte, error) {
	switch n.Kind {
	case payentity.NoticeKindPayment:
		return json.Marshal(paymentNoticeBody{BizNo: n.BizNo, PaymentID: n.PaymentID})
	case payentity.NoticeKindRefund:
		return json.Marshal(refundNoticeBody{RefundID: n.RefundID, PaymentID: n.PaymentID})
	default:
		return nil, fmt.Errorf("unknown notice kind %d", n.Kind)
	}
}
