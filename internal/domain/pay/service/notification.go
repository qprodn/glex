package service

import (
	"context"

	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
)

// Notification is the notify-log domain service: the write-once evidence
// stream's only keeper (ADR 0020). The (channel, notify_id) partial
// unique is the dedup — the bool it yields is what makes redeliveries
// idempotent.
type Notification interface {
	// Record inserts one notification's evidence write-once under the
	// caller-minted ID. isNew is false when the (channel, notify_id) pair
	// already exists — a duplicate delivery to ACK without re-processing.
	Record(ctx context.Context, n *payentity.ChannelNotification) (bool, error)
	// MarkProcessed flips the diagnostic flag after the state advance ran.
	MarkProcessed(ctx context.Context, id int64) error
	// NextID mints the evidence row's snowflake identity.
	NextID() int64
}

type notificationImpl struct {
	logs payrepo.NotifyLogRepo
}

// NewNotificationService creates the notify-log domain service.
func NewNotificationService(logs payrepo.NotifyLogRepo) Notification {
	return &notificationImpl{logs: logs}
}

func (s *notificationImpl) Record(ctx context.Context, n *payentity.ChannelNotification) (bool, error) {
	return s.logs.Insert(ctx, n)
}

func (s *notificationImpl) MarkProcessed(ctx context.Context, id int64) error {
	return s.logs.MarkProcessed(ctx, id)
}

func (s *notificationImpl) NextID() int64 {
	return s.logs.NextID()
}
