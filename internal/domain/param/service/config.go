package service

import (
	"context"
	"errors"
	"log/slog"

	"glex/internal/domain/param"
	"glex/internal/domain/param/entity"
	"glex/internal/domain/param/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// DeleteRequest identifies the record a delete withdraws and the actor whose
// id lands in the audit fields (ADR 0025: declared beside the consuming
// interface, so callers never reach into the repository package).
type DeleteRequest struct {
	ID      int64
	ActorID int64
}

// Configs is the param domain service behind the runtime-parameter endpoints
// and the crossdomain ConfigReader: the only path to the Runtime Parameter
// reads and writes (ADR 0005). Methods join the caller's ambient transaction;
// the Application service owns the transaction boundary. Every error a method
// returns to its caller first logs its Warn breadcrumb (ADR 0011).
type Configs interface {
	Get(ctx context.Context, id int64) (*entity.Config, error)
	// FindByKey resolves the parameter key — the consumption read the
	// crossdomain ConfigReader adapts.
	FindByKey(ctx context.Context, key string) (*entity.Config, error)
	List(ctx context.Context, q listq.Query) ([]*entity.Config, error)
	// Create validates the parameter and persists it.
	Create(ctx context.Context, c *entity.Config) (*entity.Config, error)
	// Update validates the change and persists it. The key stays editable —
	// unlike a Dict Type key (ADR 0006) — but a rename passes the same
	// uniqueness pre-check as a create.
	Update(ctx context.Context, c *entity.Config) (*entity.Config, error)
	// Delete soft-deletes the parameter, refusing for built-in ones (RuoYi:
	// 内置参数不能删除).
	Delete(ctx context.Context, req *DeleteRequest) error
}

type configsImpl struct {
	configs repository.ConfigRepo
	logger  *slog.Logger
}

// NewConfigsService creates the param domain service. configs backs the CRUD
// and the by-key consumption read.
func NewConfigsService(configs repository.ConfigRepo, logger *slog.Logger) Configs {
	return &configsImpl{
		configs: configs,
		logger:  logger.With(slog.String("component", "domain.param.svc")),
	}
}

func (s *configsImpl) Get(ctx context.Context, id int64) (*entity.Config, error) {
	c, err := s.configs.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.get", err)
		return nil, err
	}
	return c, nil
}

func (s *configsImpl) FindByKey(ctx context.Context, key string) (*entity.Config, error) {
	c, err := s.configs.FindByKey(ctx, key)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.find_by_key", err)
		return nil, err
	}
	return c, nil
}

func (s *configsImpl) List(ctx context.Context, q listq.Query) ([]*entity.Config, error) {
	cs, err := s.configs.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.list", err)
		return nil, err
	}
	return cs, nil
}

func (s *configsImpl) Create(ctx context.Context, c *entity.Config) (*entity.Config, error) {
	if err := c.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.create", err)
		return nil, err
	}
	if err := s.assertKeyAvailable(ctx, c.ConfigKey, 0); err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.create", err)
		return nil, err
	}
	created, err := s.configs.Create(ctx, c)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.create", err)
		return nil, err
	}
	return created, nil
}

func (s *configsImpl) Update(ctx context.Context, c *entity.Config) (*entity.Config, error) {
	if _, err := s.configs.FindByID(ctx, c.ID); err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.update", err)
		return nil, err
	}
	if err := c.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.update", err)
		return nil, err
	}
	if err := s.assertKeyAvailable(ctx, c.ConfigKey, c.ID); err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.update", err)
		return nil, err
	}
	updated, err := s.configs.Update(ctx, c)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.update", err)
		return nil, err
	}
	return updated, nil
}

func (s *configsImpl) Delete(ctx context.Context, req *DeleteRequest) error {
	stored, err := s.configs.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.delete", err)
		return err
	}
	if stored.BuiltIn {
		err := param.ErrConfigBuiltInLocked
		logx.Propagate(ctx, s.logger, "param.svc.delete", err)
		return err
	}
	if err := s.configs.Delete(ctx, &repository.DeleteRequest{ID: req.ID, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "param.svc.delete", err)
		return err
	}
	return nil
}

// assertKeyAvailable checks the key is not taken by another active parameter,
// ignoring the record with excludeID (0 = none, i.e. a create). Soft-deleted
// keys are reusable; the partial unique index remains the race-proof last
// line of defense behind this friendly pre-check. Its caller's method
// boundary owns the breadcrumb (ADR 0011).
func (s *configsImpl) assertKeyAvailable(ctx context.Context, key string, excludeID int64) error {
	existing, err := s.configs.FindByKey(ctx, key)
	if err != nil {
		if errors.Is(err, param.ErrConfigNotFound) {
			return nil
		}
		return err
	}
	if existing.ID != excludeID {
		return param.ErrConfigKeyConflict
	}
	return nil
}
