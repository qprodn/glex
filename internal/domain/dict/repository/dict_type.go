package repository

import (
	"context"

	"glex/internal/domain/dict/entity"
	"glex/pkg/listq"
)

// DictTypeRepo is the repository interface for Dict Types (the inversion
// seam declared in domain, implemented in infra).
type DictTypeRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.DictType, error)
	// FindByKey resolves the vocabulary key; the uniqueness pre-check and
	// every "does this type exist" guard read through it.
	FindByKey(ctx context.Context, key string) (*entity.DictType, error)
	List(ctx context.Context, q listq.Query) ([]*entity.DictType, error)
	Create(ctx context.Context, t *entity.DictType) (*entity.DictType, error)
	Update(ctx context.Context, t *entity.DictType) (*entity.DictType, error)
	// Delete soft-deletes the type, stamping actorID into the audit fields
	// (ADR 0002 — no row is hard-deleted).
	Delete(ctx context.Context, id, actorID int64) error
}
