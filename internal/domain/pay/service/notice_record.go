package service

import (
	"context"
	"time"

	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
)

// insertDueNotice stores the Business Notification that commits with the
// money change. An empty copied address stores nothing. A duplicate pair
// is not an error. Any other insert error is returned so the caller's
// transaction rolls the money change back with it.
func insertDueNotice(ctx context.Context, notices payrepo.BusinessNoticeRepo, n *payentity.BusinessNotice) error {
	if n == nil || n.NotifyURL == "" {
		return nil
	}
	n.ID = notices.NextID()
	n.Status = payentity.NoticeWaiting
	n.AttemptCount = 0
	n.NextAttemptAt = time.Now()
	_, err := notices.Insert(ctx, n)
	return err
}
