package repository

import (
	"context"
	"time"

	"glex/internal/domain/pay/entity"
)

// RefundRepo is the repository interface for Refund rows. State advances
// are CAS from Pending (bool false = already settled — a duplicate or
// out-of-order receipt, a no-op by design).
type RefundRepo interface {
	// FindByID resolves one active Refund.
	FindByID(ctx context.Context, id int64) (*entity.Refund, error)
	List(ctx context.Context, opts ...ListOption) ([]*entity.Refund, error)
	// Create inserts the row; the caller mints the ID (NextID).
	Create(ctx context.Context, r *entity.Refund) (*entity.Refund, error)
	// MarkSuccess CASes Pending → Success with the Channel's evidence.
	// RefundedAt zero value means "now".
	MarkSuccess(ctx context.Context, id int64, channelRefundNo string, refundedAt time.Time) (bool, error)
	// MarkFailed CASes Pending → Failed with the reason.
	MarkFailed(ctx context.Context, id int64, reason string) (bool, error)
	// NextID mints the snowflake row ID (the Refund number).
	NextID() int64
}
