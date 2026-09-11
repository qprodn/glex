package repository

import (
	"context"

	"glex/internal/domain/pay/entity"
)

// NotifyLogRepo is the repository interface for the write-once Channel
// Notification stream (ADR 0020). The only write is the insert; the only
// later change a row takes is the processed flag after its state advance
// ran. The (channel, notify_id) partial unique absorbs the channels'
// guaranteed redelivery.
type NotifyLogRepo interface {
	// Insert records one notification write-once under the caller-minted
	// ID (NextID). ok is false when the (channel, notify_id) pair already
	// exists — a duplicate delivery to ACK without re-processing, never
	// an error.
	Insert(ctx context.Context, n *entity.ChannelNotification) (ok bool, err error)
	// MarkProcessed flips the processed flag after the state advance ran.
	MarkProcessed(ctx context.Context, id int64) error
	// NextID mints the snowflake row ID.
	NextID() int64
}
