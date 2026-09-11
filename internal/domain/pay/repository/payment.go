package repository

import (
	"context"

	"glex/internal/domain/pay/entity"
)

// PaymentRepo is the repository interface for Payment rows. Every state
// transition is a compare-and-set from Pending (the bool result is false
// when the row was no longer Pending — an out-of-order or duplicate
// receipt, a no-op by design, never an error), and terminal states are
// irreversible. Refunds only ever add to RefundedAmount, atomically, on
// rows still Paid.
type PaymentRepo interface {
	// FindByID resolves one active Payment.
	FindByID(ctx context.Context, id int64) (*entity.Payment, error)
	// FindLatestByBizNo resolves a merchant's newest active Payment for
	// one Business Order Number — what a business caller reading a
	// payment's outcome wants. NotFound when none exists.
	FindLatestByBizNo(ctx context.Context, merchantID int64, bizNo string) (*entity.Payment, error)
	List(ctx context.Context, opts ...ListOption) ([]*entity.Payment, error)
	// Create inserts the row; the caller mints the ID (NextID) and owns
	// any transaction.
	Create(ctx context.Context, p *entity.Payment) (*entity.Payment, error)
	// ClosePendingByBizNo closes every still-Pending Payment of one
	// merchant's Business Order Number (the re-initiation rule) and
	// returns how many rows it closed.
	ClosePendingByBizNo(ctx context.Context, merchantID int64, bizNo string, actorID int64) (int, error)
	// SetChannelPayload stores the Channel prepay payload (code_url and
	// kin) so the QR can be re-rendered without a fresh prepay.
	SetChannelPayload(ctx context.Context, id int64, payload string) error
	// SetFailReason records why a Pending Payment died (the prepay was
	// rejected); used together with MarkFailed.
	SetFailReason(ctx context.Context, id int64, reason string) error
	// MarkPaid CASes Pending → Paid with the Channel's success evidence.
	// PaidAt zero value means "now".
	MarkPaid(ctx context.Context, id int64, capture entity.PaidCapture) (bool, error)
	// BackfillPaidInfo completes the payer evidence on an already-Paid row
	// whose earlier confirmation lacked it (independent CAS, ADR design §5).
	BackfillPaidInfo(ctx context.Context, id int64, capture entity.PaidCapture) (bool, error)
	// MarkFailed CASes Pending → Failed with the reason.
	MarkFailed(ctx context.Context, id int64, reason string) (bool, error)
	// MarkClosed CASes Pending → Closed (manual close or auto close on
	// re-initiation).
	MarkClosed(ctx context.Context, id int64) (bool, error)
	// MarkExpired CASes Pending → Expired (lazy expiry on settlement paths).
	MarkExpired(ctx context.Context, id int64) (bool, error)
	// NextID mints the snowflake row ID (the Payment number).
	NextID() int64
}
