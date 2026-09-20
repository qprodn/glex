package dictinfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/dict"
	dictentity "glex/internal/domain/dict/entity"
	dictrepo "glex/internal/domain/dict/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/dicttype"
	"glex/internal/infra/store"
	"glex/pkg/listq"
)

// toBizDictType maps an ent DictType PO to a dictentity.DictType DO.
func toBizDictType(po *ent.DictType) *dictentity.DictType {
	return &dictentity.DictType{
		ID:        po.ID,
		DictName:  po.DictName,
		DictType:  po.DictType,
		Remark:    po.Remark,
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
	}
}

type dictTypeRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewDictTypeRepo creates a new DictTypeRepo instance.
func NewDictTypeRepo(data *store.Data) dictrepo.DictTypeRepo {
	return &dictTypeRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.dict.dict_type")),
	}
}

// dictStoreError is the log-and-map for unexpected dict-store failures (ADR
// 0007): the raw error is logged once at the handling site with its operation
// and returned as the module's sentinel, so no raw ent error crosses the
// repository boundary. Expected business conditions (ent.IsNotFound and
// friends) map silently at the call sites; a canceled request is benign and
// returns the sentinel without logging.
func dictStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return dict.ErrDictStoreUnavailable
	}
	logger.ErrorContext(ctx, "dict store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return dict.ErrDictStoreUnavailable
}

func (r *dictTypeRepo) FindByID(ctx context.Context, id int64) (*dictentity.DictType, error) {
	po, err := r.data.DB.DictType(ctx).Query().
		Where(dicttype.IDEQ(id), dicttype.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, dict.ErrDictTypeNotFound
		}
		return nil, dictStoreError(ctx, r.logger, "dict.type.find_by_id", err)
	}
	return toBizDictType(po), nil
}

func (r *dictTypeRepo) FindByKey(ctx context.Context, key string) (*dictentity.DictType, error) {
	po, err := r.data.DB.DictType(ctx).Query().
		Where(dicttype.DictTypeEQ(key), dicttype.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, dict.ErrDictTypeNotFound
		}
		return nil, dictStoreError(ctx, r.logger, "dict.type.find_by_key", err)
	}
	return toBizDictType(po), nil
}

func (r *dictTypeRepo) List(ctx context.Context, q listq.Query) ([]*dictentity.DictType, error) {
	pos, err := r.data.DB.DictType(ctx).Query().
		Where(
			dicttype.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, dictStoreError(ctx, r.logger, "dict.type.list", err)
	}
	types := make([]*dictentity.DictType, 0, len(pos))
	for _, po := range pos {
		types = append(types, toBizDictType(po))
	}
	return types, nil
}

// CreateDictType inserts the type row. The caller owns the transaction; the
// partial unique index on dict_type is the race-proof backstop behind the
// service's friendly pre-check.
func (r *dictTypeRepo) Create(ctx context.Context, t *dictentity.DictType) (*dictentity.DictType, error) {
	po, err := r.data.DB.DictType(ctx).Create().
		SetID(newSnowflakeID()).
		SetDictName(t.DictName).
		SetDictType(t.DictType).
		SetRemark(t.Remark).
		SetCreateBy(t.CreateBy).
		SetUpdateBy(t.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, dictStoreError(ctx, r.logger, "dict.type.create", err)
	}
	return toBizDictType(po), nil
}

// UpdateDictType applies the updatable fields (dict_name, remark; the key is
// immutable — ADR 0006 — and never set here). The caller owns the
// transaction.
func (r *dictTypeRepo) Update(ctx context.Context, t *dictentity.DictType) (*dictentity.DictType, error) {
	po, err := r.data.DB.DictType(ctx).UpdateOneID(t.ID).
		SetDictName(t.DictName).
		SetRemark(t.Remark).
		SetUpdateBy(t.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, dict.ErrDictTypeNotFound
		}
		return nil, dictStoreError(ctx, r.logger, "dict.type.update", err)
	}
	return toBizDictType(po), nil
}

// DeleteDictType soft-deletes the type (ADR 0002 — no row is hard-deleted),
// stamping actorID into the audit fields. The caller owns the transaction.
func (r *dictTypeRepo) Delete(ctx context.Context, id, actorID int64) error {
	err := r.data.DB.DictType(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return dict.ErrDictTypeNotFound
		}
		return dictStoreError(ctx, r.logger, "dict.type.delete", err)
	}
	return nil
}
