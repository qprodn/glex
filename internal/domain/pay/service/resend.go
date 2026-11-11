package service

import (
	"context"
	"log/slog"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// NoticeDesk is the back-office surface for exhausted Business
// Notifications. Manual send is one call and one attempt. It is not part
// of DeliverDue and it does not restart the automatic schedule.
type NoticeDesk interface {
	// ListExhausted returns the work queue: exhausted notices, each with
	// its latest attempt. Accepted notices and notices still waiting are
	// absent. Their attempt rows stay stored.
	ListExhausted(ctx context.Context, q listq.Query) ([]*ExhaustedNotice, error)
	Resend(ctx context.Context, req ResendNoticeRequest) (*ResendNoticeResult, error)
}

// ResendNoticeRequest is one manual send of an exhausted notice.
type ResendNoticeRequest struct {
	ID int64
	// Now stamps the attempt. The automatic schedule is not rewritten.
	Now time.Time
	// ActorID stamps the attempt's audit fields (0 = the system itself).
	ActorID int64
}

// ExhaustedNotice is one exhausted Business Notification and the latest
// attempt recorded for it. Last is nil when the notice has no attempt row.
// The repository keeps its own pair; this is the domain value callers see.
type ExhaustedNotice struct {
	Notice *payentity.BusinessNotice
	Last   *payentity.BusinessNoticeAttempt
}

// ResendNoticeResult is the notice after the attempt and the attempt itself.
// A failed HTTP call is the attempt's outcome: Result is set and the error
// is nil. A refusal returns the sentinel and no attempt.
type ResendNoticeResult struct {
	Notice  *payentity.BusinessNotice
	Attempt *payentity.BusinessNoticeAttempt
}

type noticeDeskImpl struct {
	notices payrepo.BusinessNoticeRepo
	apps    payrepo.PayApplicationRepo
	poster  NoticePoster
	logger  *slog.Logger
}

// NewNoticeDesk creates the manual-resend service. The poster is the same
// HTTP boundary automatic delivery uses.
func NewNoticeDesk(
	notices payrepo.BusinessNoticeRepo,
	apps payrepo.PayApplicationRepo,
	poster NoticePoster,
	logger *slog.Logger,
) NoticeDesk {
	return &noticeDeskImpl{
		notices: notices,
		apps:    apps,
		poster:  poster,
		logger:  logger.With(slog.String("component", "domain.pay.notice.resend")),
	}
}

// ListExhausted returns exhausted notices with the latest attempt on each.
func (s *noticeDeskImpl) ListExhausted(ctx context.Context, q listq.Query) ([]*ExhaustedNotice, error) {
	rows, err := s.notices.ListExhausted(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.list_exhausted", err)
		return nil, err
	}
	out := make([]*ExhaustedNotice, 0, len(rows))
	for _, row := range rows {
		out = append(out, &ExhaustedNotice{Notice: row.Notice, Last: row.Last})
	}
	return out, nil
}

// Resend delivers one exhausted notice to the Pay Application's current
// result address of the same kind. Waiting and accepted notices are
// refused. An empty current address is refused and records no attempt.
// HTTP 2xx accepts the notice. Any other outcome, including timeout and
// connection failure, leaves it exhausted. AttemptCount and the next
// automatic time are left alone. A disabled or withdrawn Pay Application
// still resolves, because the address is read with FindByIDAny.
func (s *noticeDeskImpl) Resend(ctx context.Context, req ResendNoticeRequest) (*ResendNoticeResult, error) {
	n, err := s.notices.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", err)
		return nil, err
	}
	if n.Status != payentity.NoticeExhausted {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", pay.ErrBusinessNoticeNotExhausted)
		return nil, pay.ErrBusinessNoticeNotExhausted
	}
	app, err := s.apps.FindByIDAny(ctx, n.ApplicationID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", err)
		return nil, err
	}
	url, ok := currentResultAddress(app, n.Kind)
	if !ok {
		s.logger.Error("business notice kind is unknown",
			slog.String("operation", "pay.svc.notice.resend"),
			slog.Int64("notice_id", n.ID),
			slog.Int64("kind", int64(n.Kind)))
		return nil, pay.ErrPayStoreUnavailable
	}
	if url == "" {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", pay.ErrBusinessNoticeAddressEmpty)
		return nil, pay.ErrBusinessNoticeAddressEmpty
	}
	body, err := noticeBody(n)
	if err != nil {
		s.logger.Error("business notice body marshal failed",
			slog.String("operation", "pay.svc.notice.resend"),
			slog.Int64("notice_id", n.ID),
			slog.Any("error", err))
		return nil, pay.ErrPayStoreUnavailable
	}
	if err := ctx.Err(); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", err)
		return nil, err
	}
	result, postErr := s.poster.Post(ctx, NoticePost{URL: url, Body: body})
	if err := ctx.Err(); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", err)
		return nil, err
	}
	attempt := &payentity.BusinessNoticeAttempt{
		ID:        s.notices.NextID(),
		NoticeID:  n.ID,
		Manual:    true,
		CreatedAt: req.Now,
	}
	if postErr != nil {
		attempt.Error = clipAttemptError(postErr.Error())
	} else {
		code := result.StatusCode
		attempt.HTTPStatus = &code
		attempt.Accepted = code >= 200 && code < 300
	}
	if attempt.Accepted {
		n.Status = payentity.NoticeAccepted
	}
	if err := s.notices.RecordManualAttempt(ctx, payrepo.RecordManualAttemptRequest{
		Notice:  n,
		Attempt: attempt,
		ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notice.resend", err)
		return nil, err
	}
	return &ResendNoticeResult{Notice: n, Attempt: attempt}, nil
}

func currentResultAddress(app *payentity.PayApplication, kind payentity.NoticeKind) (string, bool) {
	switch kind {
	case payentity.NoticeKindPayment:
		return app.OrderNotifyURL, true
	case payentity.NoticeKindRefund:
		return app.RefundNotifyURL, true
	default:
		return "", false
	}
}
