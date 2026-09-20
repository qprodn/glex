package repository

import (
	"context"

	"glex/internal/domain/pay/entity"
	"glex/pkg/listq"
)

// UpdateMerchantRequest is one editable-fields rewrite: the account with its
// resealed config and the actor stamped into the audit fields.
type UpdateMerchantRequest struct {
	Merchant *entity.MerchantAccount
	ActorID  int64
}

// UpdateMerchantStatusRequest is one enable-switch flip: the account, the
// new status, and the actor stamped into the audit fields.
type UpdateMerchantStatusRequest struct {
	ID      int64
	Status  entity.MerchantStatus
	ActorID int64
}

// DeleteMerchantRequest is one withdrawal (ADR 0002): the account to
// soft-delete and the actor stamped into the audit fields.
type DeleteMerchantRequest struct {
	ID      int64
	ActorID int64
}

// MerchantAccountRepo is the repository interface for Merchant Account rows
// (the inversion seam declared in domain, implemented in infra). The repo
// boundary owns the config envelope: writes encrypt Config whole (ADR 0018)
// alongside the caller-computed masked echo and digest; reads return the
// ciphertext and the masked echo and never decrypt — LoadConfig decrypts
// explicitly for the paths that resolve gateways or rewrite configs.
type MerchantAccountRepo interface {
	// FindByID resolves one active Merchant Account. Config stays nil —
	// display never decrypts.
	FindByID(ctx context.Context, id int64) (*entity.MerchantAccount, error)
	// FindByIDAny resolves the account regardless of withdrawal — the
	// settlement paths (prepay, query, close, notify) keep resolving a
	// withdrawn merchant's snapshot channel (design §7).
	FindByIDAny(ctx context.Context, id int64) (*entity.MerchantAccount, error)
	List(ctx context.Context, q listq.Query) ([]*entity.MerchantAccount, error)
	// LoadConfig decrypts the account's stored config into Config. Fails
	// with the master-key sentinel when PAY_MASTER_KEY is absent (ADR 0018).
	LoadConfig(ctx context.Context, m *entity.MerchantAccount) error
	// Create inserts the row: Config is encrypted whole; ConfigMasked and
	// ConfigDigest must be set by the caller (they are schema-derived).
	Create(ctx context.Context, m *entity.MerchantAccount) (*entity.MerchantAccount, error)
	// Update rewrites req.Merchant's editable fields — name, config
	// (encrypted whole with fresh masked echo and digest), remark —
	// stamping req.ActorID.
	Update(ctx context.Context, req UpdateMerchantRequest) (*entity.MerchantAccount, error)
	// UpdateStatus flips the enable switch and stamps req.ActorID.
	UpdateStatus(ctx context.Context, req UpdateMerchantStatusRequest) error
	// Delete soft-deletes the row, stamping req.ActorID (ADR 0002).
	Delete(ctx context.Context, req DeleteMerchantRequest) error
	// NextID mints the snowflake row ID.
	NextID() int64
}
