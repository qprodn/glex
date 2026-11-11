package repository

import (
	"context"

	"glex/internal/domain/pay/entity"
	"glex/pkg/listq"
)

// UpdatePayApplicationRequest is one display-field rewrite: the Pay
// Application and the actor stamped into the audit fields. The Merchant
// Account binding is not an editable field.
type UpdatePayApplicationRequest struct {
	App     *entity.PayApplication
	ActorID int64
}

// UpdatePayApplicationStatusRequest is one enable-switch flip.
type UpdatePayApplicationStatusRequest struct {
	ID      int64
	Status  entity.ApplicationStatus
	ActorID int64
}

// DeletePayApplicationRequest is one withdrawal (ADR 0002): the Pay
// Application to soft-delete and the actor stamped into the audit fields.
type DeletePayApplicationRequest struct {
	ID      int64
	ActorID int64
}

// PayApplicationRepo is the repository interface for Pay Application rows
// (the inversion seam declared in domain, implemented in infra).
type PayApplicationRepo interface {
	// FindByID resolves one Pay Application that is not withdrawn.
	FindByID(ctx context.Context, id int64) (*entity.PayApplication, error)
	// FindByIDAny resolves the Pay Application regardless of withdrawal.
	// Manual resend reads the current result address from a disabled or
	// withdrawn row. A disabled row is still live for FindByID.
	FindByIDAny(ctx context.Context, id int64) (*entity.PayApplication, error)
	// List returns Pay Applications that are not withdrawn.
	List(ctx context.Context, q listq.Query) ([]*entity.PayApplication, error)
	// Create inserts the row. A display name already held by a Pay
	// Application that is not withdrawn comes back as the duplicate sentinel.
	Create(ctx context.Context, app *entity.PayApplication) (*entity.PayApplication, error)
	// Update rewrites the display name and both result addresses, stamping
	// req.ActorID. A display name already held by another live Pay
	// Application comes back as the duplicate sentinel.
	Update(ctx context.Context, req UpdatePayApplicationRequest) (*entity.PayApplication, error)
	// UpdateStatus flips the enable switch and stamps req.ActorID.
	UpdateStatus(ctx context.Context, req UpdatePayApplicationStatusRequest) error
	// Delete soft-deletes the row, stamping req.ActorID (ADR 0002). The
	// display name is free for another Pay Application afterward.
	Delete(ctx context.Context, req DeletePayApplicationRequest) error
	// NextID mints the snowflake row ID.
	NextID() int64
}
