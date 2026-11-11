package repository

import (
	"context"
	"time"

	"glex/internal/domain/pay/entity"
	"glex/pkg/listq"
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
	// ListDue returns waiting notices whose next attempt time is due.
	ListDue(ctx context.Context, req ListDueNoticesRequest) ([]*entity.BusinessNotice, error)
	// ClaimDueNotice moves a waiting, due notice's next time forward so a
	// second scan does not take it. ok is false when the row was not
	// waiting and due.
	ClaimDueNotice(ctx context.Context, req ClaimDueNoticeRequest) (ok bool, err error)
	// RecordAutomaticAttempt stores one automatic attempt and the notice
	// progress that follows it: count, status, and next time.
	RecordAutomaticAttempt(ctx context.Context, req RecordAutomaticAttemptRequest) error
	// FindByID resolves one Business Notification that is not withdrawn.
	FindByID(ctx context.Context, id int64) (*entity.BusinessNotice, error)
	// RecordManualAttempt stores one manual attempt and the notice status
	// it leaves. The automatic attempt count and the next automatic time
	// stay as they are.
	RecordManualAttempt(ctx context.Context, req RecordManualAttemptRequest) error
	// ListExhausted returns exhausted notices, each with its latest
	// attempt. Waiting and accepted notices are not included. Attempt
	// rows are not removed.
	ListExhausted(ctx context.Context, q listq.Query) ([]*ExhaustedBusinessNotice, error)
}

// ExhaustedBusinessNotice is one exhausted notice and the latest attempt
// recorded for it. Last is nil when the notice has no attempt row.
type ExhaustedBusinessNotice struct {
	Notice *entity.BusinessNotice
	Last   *entity.BusinessNoticeAttempt
}

// ListDueNoticesRequest selects waiting notices due at Now.
type ListDueNoticesRequest struct {
	Now   time.Time
	Limit int
}

// ClaimDueNoticeRequest leases one due notice until Until.
type ClaimDueNoticeRequest struct {
	ID    int64
	Now   time.Time
	Until time.Time
}

// RecordAutomaticAttemptRequest is one finished automatic attempt plus the
// notice fields it leaves behind. A zero NextAttemptAt clears the schedule.
type RecordAutomaticAttemptRequest struct {
	Notice  *entity.BusinessNotice
	Attempt *entity.BusinessNoticeAttempt
}

// RecordManualAttemptRequest is one finished manual attempt plus the notice
// status it leaves. AttemptCount and NextAttemptAt are not fields of this
// write: a manual send does not restart the automatic schedule.
type RecordManualAttemptRequest struct {
	Notice  *entity.BusinessNotice
	Attempt *entity.BusinessNoticeAttempt
	ActorID int64
}
