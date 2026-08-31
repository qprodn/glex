package dictinfra

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/dict"
	dictentity "glex/internal/domain/dict/entity"
	dictrepo "glex/internal/domain/dict/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/dictdata"
	"glex/internal/infra/store"
)

// toBizDictEntry maps an ent DictData PO to a dictentity.DictEntry DO. The
// domain noun is Dict Entry; the table keeps RuoYi's sys_dict_data shape,
// with tag_style stored as RuoYi's list_class column.
func toBizDictEntry(po *ent.DictData) *dictentity.DictEntry {
	return &dictentity.DictEntry{
		ID:        po.ID,
		DictType:  po.DictType,
		DictLabel: po.DictLabel,
		DictValue: po.DictValue,
		DictSort:  po.DictSort,
		TagStyle:  po.ListClass,
		IsDefault: po.IsDefault,
		Remark:    po.Remark,
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
	}
}

type dictEntryRepo struct {
	data *store.Data
}

// NewDictEntryRepo creates a new DictEntryRepo instance.
func NewDictEntryRepo(data *store.Data) dictrepo.DictEntryRepo {
	return &dictEntryRepo{data: data}
}

func (r *dictEntryRepo) FindByID(ctx context.Context, id int64) (*dictentity.DictEntry, error) {
	po, err := r.data.DB.DictData(ctx).Query().
		Where(dictdata.IDEQ(id), dictdata.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, dict.ErrDictEntryNotFound
		}
		return nil, err
	}
	return toBizDictEntry(po), nil
}

func (r *dictEntryRepo) FindByTypeAndValue(ctx context.Context, dictType, value string) (*dictentity.DictEntry, error) {
	po, err := r.data.DB.DictData(ctx).Query().
		Where(dictdata.DictTypeEQ(dictType), dictdata.DictValueEQ(value), dictdata.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, dict.ErrDictEntryNotFound
		}
		return nil, err
	}
	return toBizDictEntry(po), nil
}

func (r *dictEntryRepo) List(ctx context.Context, opts ...dictrepo.ListOption) ([]*dictentity.DictEntry, error) {
	o := dictrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.DictData(ctx).Query().
		Where(
			dictdata.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]*dictentity.DictEntry, 0, len(pos))
	for _, po := range pos {
		entries = append(entries, toBizDictEntry(po))
	}
	return entries, nil
}

// ListByDictKey reads one type's active entries in (dict_sort, id) order —
// the shape the options endpoint serves. An unknown key matches nothing and
// yields an empty list.
func (r *dictEntryRepo) ListByDictKey(ctx context.Context, key string) ([]*dictentity.DictEntry, error) {
	pos, err := r.data.DB.DictData(ctx).Query().
		Where(dictdata.DictTypeEQ(key), dictdata.DeletedAtIsNil()).
		Order(dictdata.ByDictSort(), dictdata.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]*dictentity.DictEntry, 0, len(pos))
	for _, po := range pos {
		entries = append(entries, toBizDictEntry(po))
	}
	return entries, nil
}

func (r *dictEntryRepo) CountByDictKey(ctx context.Context, key string) (int, error) {
	return r.data.DB.DictData(ctx).Query().
		Where(dictdata.DictTypeEQ(key), dictdata.DeletedAtIsNil()).
		Count(ctx)
}

// CreateDictEntry inserts the entry row. The caller owns the transaction;
// the partial unique index on (dict_type, dict_value) is the race-proof
// backstop behind the service's friendly pre-check.
func (r *dictEntryRepo) Create(ctx context.Context, e *dictentity.DictEntry) (*dictentity.DictEntry, error) {
	po, err := r.data.DB.DictData(ctx).Create().
		SetID(newSnowflakeID()).
		SetDictType(e.DictType).
		SetDictLabel(e.DictLabel).
		SetDictValue(e.DictValue).
		SetDictSort(e.DictSort).
		SetListClass(e.TagStyle).
		SetIsDefault(e.IsDefault).
		SetRemark(e.Remark).
		SetCreateBy(e.CreateBy).
		SetUpdateBy(e.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toBizDictEntry(po), nil
}

// UpdateDictEntry applies the updatable fields (the owning dict_type is
// immutable — ADR 0006 — and never set here). The caller owns the
// transaction.
func (r *dictEntryRepo) Update(ctx context.Context, e *dictentity.DictEntry) (*dictentity.DictEntry, error) {
	po, err := r.data.DB.DictData(ctx).UpdateOneID(e.ID).
		SetDictLabel(e.DictLabel).
		SetDictValue(e.DictValue).
		SetDictSort(e.DictSort).
		SetListClass(e.TagStyle).
		SetIsDefault(e.IsDefault).
		SetRemark(e.Remark).
		SetUpdateBy(e.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, dict.ErrDictEntryNotFound
		}
		return nil, err
	}
	return toBizDictEntry(po), nil
}

// DeleteDictEntry soft-deletes the entry (ADR 0002 — no row is hard-deleted),
// stamping actorID into the audit fields. The caller owns the transaction.
func (r *dictEntryRepo) Delete(ctx context.Context, id, actorID int64) error {
	err := r.data.DB.DictData(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return dict.ErrDictEntryNotFound
		}
		return err
	}
	return nil
}
