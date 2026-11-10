package repository

import (
	"context"
	"time"

	"glex/internal/domain/pay/entity"
	"glex/pkg/listq"
)

// FindByBizNoRequest names one Pay Application's Business Order Number.
// The pair identifies at most one Payment that is not withdrawn.
type FindByBizNoRequest struct {
	ApplicationID int64
	BizNo         string
}

// ListPendingExpiredRequest is one expire-sweep scan (ADR 0023): the expiry
// horizon, the IDGT cursor (0 = from the oldest), and the page size.
type ListPendingExpiredRequest struct {
	Before  time.Time
	AfterID int64
	Limit   int
}

// SetChannelPayloadRequest stores one Payment's Channel prepay payload.
type SetChannelPayloadRequest struct {
	ID      int64
	Payload string
}

// SetFailReasonRequest records why one Pending Payment died.
type SetFailReasonRequest struct {
	ID     int64
	Reason string
}

// MarkPaidRequest is one Pending → Paid CAS: the Payment and the Channel's
// success evidence.
type MarkPaidRequest struct {
	ID      int64
	Capture entity.PaidCapture
}

// BackfillPaidInfoRequest completes one already-Paid Payment's payer
// evidence.
type BackfillPaidInfoRequest struct {
	ID      int64
	Capture entity.PaidCapture
}

// MarkPaymentFailedRequest is one Payment's Pending → Failed CAS with the
// reason.
type MarkPaymentFailedRequest struct {
	ID     int64
	Reason string
}

// PaymentRepo is the repository interface for Payment rows. Every state
// transition is a compare-and-set from Pending (the bool result is false
// when the row was no longer Pending — an out-of-order or duplicate
// receipt, a no-op by design, never an error), and terminal states are
// irreversible. Refunds only ever add to RefundedAmount, atomically, on
// rows still Paid.
type PaymentRepo interface {
	// FindByID resolves one active Payment.
	FindByID(ctx context.Context, id int64) (*entity.Payment, error)
	// FindByBizNo resolves the Pay Application's Payment for one Business
	// Order Number. NotFound when none exists. At most one row matches.
	FindByBizNo(ctx context.Context, req FindByBizNoRequest) (*entity.Payment, error)
	// ListPendingExpired returns up to req.Limit still-Pending Payments
	// whose expiry has passed before req.Before, id ascending from
	// req.AfterID (0 = from the oldest) — the expire-sweep's candidate set
	// (ADR 0023). The cursor lets one sweep run page past rows that
	// permanently fail to settle, so a poisoned head cannot monopolize
	// every batch.
	ListPendingExpired(ctx context.Context, req ListPendingExpiredRequest) ([]*entity.Payment, error)
	List(ctx context.Context, q listq.Query) ([]*entity.Payment, error)
	// Create inserts the row; the caller mints the ID (NextID) and owns
	// any transaction.
	Create(ctx context.Context, p *entity.Payment) (*entity.Payment, error)
	// SetChannelPayload stores the Channel prepay payload (code_url and
	// kin) so the QR can be re-rendered without a fresh prepay.
	SetChannelPayload(ctx context.Context, req SetChannelPayloadRequest) error
	// SetFailReason records why a Pending Payment died (the prepay was
	// rejected); used together with MarkFailed.
	SetFailReason(ctx context.Context, req SetFailReasonRequest) error
	// MarkPaid CASes Pending → Paid with the Channel's success evidence.
	// PaidAt zero value means "now".
	MarkPaid(ctx context.Context, req MarkPaidRequest) (bool, error)
	// BackfillPaidInfo completes the payer evidence on an already-Paid row
	// whose earlier confirmation lacked it (independent CAS, ADR design §5).
	BackfillPaidInfo(ctx context.Context, req BackfillPaidInfoRequest) (bool, error)
	// MarkFailed CASes Pending → Failed with the reason.
	MarkFailed(ctx context.Context, req MarkPaymentFailedRequest) (bool, error)
	// MarkClosed CASes Pending → Closed (manual close).
	MarkClosed(ctx context.Context, id int64) (bool, error)
	// MarkExpired CASes Pending → Expired (lazy expiry on settlement paths).
	MarkExpired(ctx context.Context, id int64) (bool, error)
	// NextID mints the snowflake row ID (the Payment number).
	NextID() int64
}
