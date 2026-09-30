package paraminfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/param"
	paramentity "glex/internal/domain/param/entity"
	paramrepo "glex/internal/domain/param/repository"
	"glex/internal/infra/ent"
	entparam "glex/internal/infra/ent/param"
	"glex/internal/infra/store"
	"glex/pkg/listq"
)

// toBizConfig maps an ent Param PO to a paramentity.Config DO. The built_in
// smallint switch (RuoYi's char Y/N, normalized) becomes the entity's bool.
func toBizConfig(po *ent.Param) *paramentity.Config {
	return &paramentity.Config{
		ID:          po.ID,
		ConfigName:  po.ConfigName,
		ConfigKey:   po.ConfigKey,
		ConfigValue: po.ConfigValue,
		BuiltIn:     po.BuiltIn != 0,
		Remark:      po.Remark,
		CreateBy:    po.CreateBy,
		UpdateBy:    po.UpdateBy,
		CreatedAt:   po.CreatedAt,
		UpdatedAt:   po.UpdatedAt,
	}
}

// builtInColumn is the smallint the built_in flag stores (1 = built-in, the
// system assumes the parameter exists).
const builtInColumn = 1

type configRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewConfigRepo creates a new ConfigRepo instance.
func NewConfigRepo(data *store.Data) paramrepo.ConfigRepo {
	return &configRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.param.config")),
	}
}

// paramStoreError is the log-and-map for unexpected param-store failures (ADR
// 0007): the raw error is logged once at the handling site with its operation
// and returned as the module's sentinel, so no raw ent error crosses the
// repository boundary. Expected business conditions (ent.IsNotFound and
// friends) map silently at the call sites; a canceled request is benign and
// returns the sentinel without logging.
func paramStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return param.ErrConfigStoreUnavailable
	}
	logger.ErrorContext(ctx, "param store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return param.ErrConfigStoreUnavailable
}

func (r *configRepo) FindByID(ctx context.Context, id int64) (*paramentity.Config, error) {
	po, err := r.data.DB.Param(ctx).Query().
		Where(entparam.IDEQ(id), entparam.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, param.ErrConfigNotFound
		}
		return nil, paramStoreError(ctx, r.logger, "param.config.find_by_id", err)
	}
	return toBizConfig(po), nil
}

func (r *configRepo) FindByKey(ctx context.Context, key string) (*paramentity.Config, error) {
	po, err := r.data.DB.Param(ctx).Query().
		Where(entparam.ConfigKeyEQ(key), entparam.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, param.ErrConfigNotFound
		}
		return nil, paramStoreError(ctx, r.logger, "param.config.find_by_key", err)
	}
	return toBizConfig(po), nil
}

func (r *configRepo) List(ctx context.Context, q listq.Query) ([]*paramentity.Config, error) {
	pos, err := r.data.DB.Param(ctx).Query().
		Where(
			entparam.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, paramStoreError(ctx, r.logger, "param.config.list", err)
	}
	configs := make([]*paramentity.Config, 0, len(pos))
	for _, po := range pos {
		configs = append(configs, toBizConfig(po))
	}
	return configs, nil
}

// Create inserts the parameter row. The caller owns the transaction; the
// partial unique index on config_key is the race-proof backstop behind the
// service's friendly pre-check.
func (r *configRepo) Create(ctx context.Context, c *paramentity.Config) (*paramentity.Config, error) {
	builtIn := 0
	if c.BuiltIn {
		builtIn = builtInColumn
	}
	po, err := r.data.DB.Param(ctx).Create().
		SetID(newSnowflakeID()).
		SetConfigName(c.ConfigName).
		SetConfigKey(c.ConfigKey).
		SetConfigValue(c.ConfigValue).
		SetBuiltIn(builtIn).
		SetRemark(c.Remark).
		SetCreateBy(c.CreateBy).
		SetUpdateBy(c.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, paramStoreError(ctx, r.logger, "param.config.create", err)
	}
	return toBizConfig(po), nil
}

// Update applies the updatable fields — all of them, the key included: a
// Config key stays editable after create (unlike a Dict Type key, ADR 0006;
// see docs/modules/param.md). The caller owns the transaction.
func (r *configRepo) Update(ctx context.Context, c *paramentity.Config) (*paramentity.Config, error) {
	builtIn := 0
	if c.BuiltIn {
		builtIn = builtInColumn
	}
	po, err := r.data.DB.Param(ctx).UpdateOneID(c.ID).
		SetConfigName(c.ConfigName).
		SetConfigKey(c.ConfigKey).
		SetConfigValue(c.ConfigValue).
		SetBuiltIn(builtIn).
		SetRemark(c.Remark).
		SetUpdateBy(c.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, param.ErrConfigNotFound
		}
		return nil, paramStoreError(ctx, r.logger, "param.config.update", err)
	}
	return toBizConfig(po), nil
}

// Delete soft-deletes the parameter (ADR 0002 — no row is hard-deleted),
// stamping req.ActorID into the audit fields. The caller owns the transaction.
func (r *configRepo) Delete(ctx context.Context, req *paramrepo.DeleteRequest) error {
	err := r.data.DB.Param(ctx).UpdateOneID(req.ID).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return param.ErrConfigNotFound
		}
		return paramStoreError(ctx, r.logger, "param.config.delete", err)
	}
	return nil
}
