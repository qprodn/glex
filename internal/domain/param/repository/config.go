package repository

import (
	"context"

	"glex/internal/domain/param/entity"
	"glex/pkg/listq"
)

// DeleteRequest identifies the record a delete withdraws and the actor whose
// id lands in the audit fields (ADR 0025).
type DeleteRequest struct {
	ID      int64
	ActorID int64
}

// ConfigRepo is the repository interface for Runtime Parameters (the
// inversion seam declared in domain, implemented in infra).
type ConfigRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Config, error)
	// FindByKey resolves the parameter key; the uniqueness pre-check, the
	// crossdomain ConfigReader, and every "does this parameter exist" guard
	// read through it.
	FindByKey(ctx context.Context, key string) (*entity.Config, error)
	List(ctx context.Context, q listq.Query) ([]*entity.Config, error)
	Create(ctx context.Context, c *entity.Config) (*entity.Config, error)
	Update(ctx context.Context, c *entity.Config) (*entity.Config, error)
	// Delete soft-deletes the parameter, stamping req.ActorID into the audit
	// fields (ADR 0002 — no row is hard-deleted).
	Delete(ctx context.Context, req *DeleteRequest) error
}
