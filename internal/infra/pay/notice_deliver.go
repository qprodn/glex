package payinfra

import (
	"context"
	"fmt"
	"time"

	"glex/internal/domain/job"
	payservice "glex/internal/domain/pay/service"
)

// NoticeDeliverHandler names the business-notice delivery executor.
const NoticeDeliverHandler = "pay.notice-deliver"

// NoticeDeliverCron is the scan period. ParseStandard accepts the @every
// descriptor, so this stays off the 5-field minute grid. Ten seconds is
// inside the fifteen-second bound and matches the process tick. The expire
// sweep stays on its minute cron.
const NoticeDeliverCron = "@every 10s"

// noticeDeliverExecutor scans waiting notices that are due and posts them.
// A duplicate or missed firing is safe: delivery claims each notice before
// the HTTP call.
type noticeDeliverExecutor struct {
	delivery payservice.Delivery
}

// NewNoticeDeliverExecutor provides the delivery executor into the
// "job-executors" value group, the same wiring as the expire sweep.
func NewNoticeDeliverExecutor(delivery payservice.Delivery) job.Executor {
	return &noticeDeliverExecutor{delivery: delivery}
}

// Name implements job.Executor.
func (e *noticeDeliverExecutor) Name() string { return NoticeDeliverHandler }

// Run implements job.Executor.
func (e *noticeDeliverExecutor) Run(ctx context.Context, _ string) (string, error) {
	n, err := e.delivery.DeliverDue(ctx, payservice.DeliverDueRequest{Now: time.Now()})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("attempted %d business notice(s)", n), nil
}
