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
	// MarkSuccess settles the refund and advances the payment's refunded
	// sum as one atomic unit: Pending → Success with the Channel's
	// evidence, and — only when the CAS won — refunded_amount += amount on
	// the owning payment. The pair moves together or not at all: an
	// understated sum would let the cap check admit more than was paid.
	// RefundedAt zero value means "now"; bool false means the CAS lost
	// (a duplicate delivery) and nothing moved.
	MarkSuccess(ctx context.Context, id, paymentID, amount int64, channelRefundNo string, refundedAt time.Time) (bool, error)
	// MarkFailed CASes Pending → Failed with the reason.
	MarkFailed(ctx context.Context, id int64, reason string) (bool, error)
	// PendingRefundSum sums the amounts of a payment's still-Pending
	// refunds — the in-flight reservations the cap check must count
	// alongside the settled sum (ticket 07: async refunds must reserve
	// their amount while PENDING).
	PendingRefundSum(ctx context.Context, paymentID int64) (int64, error)
	// NextID mints the snowflake row ID (the Refund number).
	NextID() int64
}
