package repository

import (
	"context"

	"glex/internal/domain/pay/entity"
	"glex/pkg/listq"
)

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
	// Update rewrites the editable fields — name, config (encrypted whole
	// with fresh masked echo and digest), remark. Audit stamps actorID.
	Update(ctx context.Context, m *entity.MerchantAccount, actorID int64) (*entity.MerchantAccount, error)
	// UpdateStatus flips the enable switch and stamps actorID.
	UpdateStatus(ctx context.Context, id int64, status entity.MerchantStatus, actorID int64) error
	// Delete soft-deletes the row, stamping actorID (ADR 0002).
	Delete(ctx context.Context, id, actorID int64) error
	// NextID mints the snowflake row ID.
	NextID() int64
}
