package repository

import (
	"context"

	"glex/internal/domain/file/entity"
)

// FileRepo is the repository interface for Stored File rows (the inversion
// seam declared in domain, implemented in infra). The bytes live behind the
// storage port, not here.
type FileRepo interface {
	// FindByID resolves one active Stored File.
	FindByID(ctx context.Context, id int64) (*entity.StoredFile, error)
	// FindByIDs resolves active Stored Files for a batch of ids. Batch
	// semantics: unknown or withdrawn ids are simply absent from the
	// result — callers render what resolved.
	FindByIDs(ctx context.Context, ids []int64) ([]*entity.StoredFile, error)
	List(ctx context.Context, opts ...ListOption) ([]*entity.StoredFile, error)
	// Create inserts the row. The ID is minted up front (NextID) so the
	// Stored Key and the row correlate; the caller owns any transaction.
	Create(ctx context.Context, f *entity.StoredFile) (*entity.StoredFile, error)
	// Delete soft-deletes the row, stamping actorID into the audit fields
	// (ADR 0002 — no row is hard-deleted; the bytes are removed separately,
	// see ADR 0009).
	Delete(ctx context.Context, id, actorID int64) error
	// NextID mints the snowflake row ID.
	NextID() int64
}
