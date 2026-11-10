package repository

import (
	"context"

	"glex/internal/domain/pay/entity"
)

// BusinessNoticeRepo stores Business Notifications. One Payment has at most
// one payment notice, and one Refund has at most one refund notice. A
// duplicate insert is the unique index doing that job: ok is false, and
// that is not an error.
type BusinessNoticeRepo interface {
	// Insert records one notice under the caller-minted ID (NextID).
	// ok is false when that payment or refund already has its notice.
	Insert(ctx context.Context, n *entity.BusinessNotice) (ok bool, err error)
	// NextID mints the snowflake row ID.
	NextID() int64
}
