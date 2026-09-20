package repository

import (
	"context"

	"glex/internal/domain/dict/entity"
	"glex/pkg/listq"
)

// DeleteRequest identifies the row a Delete withdraws and the actor whose id
// lands in the audit fields. Both dict repos' deletes need the same shape, so
// they share the struct (ADR 0025).
type DeleteRequest struct {
	ID      int64
	ActorID int64
}

// FindByTypeAndValueRequest names the (dict_type, dict_value) pair resolved
// among active entries.
type FindByTypeAndValueRequest struct {
	DictType string
	Value    string
}

// DictEntryRepo is the repository interface for Dict Entries (the inversion
// seam declared in domain, implemented in infra).
type DictEntryRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.DictEntry, error)
	// FindByTypeAndValue resolves the (dict_type, dict_value) pair among
	// active entries; the value-uniqueness pre-check reads through it.
	FindByTypeAndValue(ctx context.Context, req *FindByTypeAndValueRequest) (*entity.DictEntry, error)
	List(ctx context.Context, q listq.Query) ([]*entity.DictEntry, error)
	// ListByDictKey returns one type's active entries ordered by
	// (dict_sort, id) — the shape the options endpoint serves. An unknown
	// key yields an empty list, not an error.
	ListByDictKey(ctx context.Context, key string) ([]*entity.DictEntry, error)
	// CountByDictKey counts a type's active entries; the type-delete guard
	// reads through it.
	CountByDictKey(ctx context.Context, key string) (int, error)
	Create(ctx context.Context, e *entity.DictEntry) (*entity.DictEntry, error)
	Update(ctx context.Context, e *entity.DictEntry) (*entity.DictEntry, error)
	// Delete soft-deletes the entry, stamping req.ActorID into the audit
	// fields (ADR 0002 — no row is hard-deleted).
	Delete(ctx context.Context, req *DeleteRequest) error
}
