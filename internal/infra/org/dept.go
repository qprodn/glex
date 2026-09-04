package orginfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"glex/internal/domain/org"
	orgentity "glex/internal/domain/org/entity"
	orgrepo "glex/internal/domain/org/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/dept"
	"glex/internal/infra/store"
)

// toBizDept maps an ent Dept PO to a orgentity.Dept DO. Children stays empty —
// tree assembly is the domain service's job (ADR 0014).
func toBizDept(po *ent.Dept) *orgentity.Dept {
	return &orgentity.Dept{
		ID:        po.ID,
		ParentID:  po.ParentID,
		DeptName:  po.DeptName,
		OrderNum:  po.OrderNum,
		Leader:    po.Leader,
		Phone:     po.Phone,
		Email:     po.Email,
		Status:    int32(po.Status),
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
	}
}

type deptRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewDeptRepo creates a new DeptRepo instance.
func NewDeptRepo(data *store.Data) orgrepo.DeptRepo {
	return &deptRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.org.dept")),
	}
}

func (r *deptRepo) FindByID(ctx context.Context, id int64) (*orgentity.Dept, error) {
	po, err := r.data.DB.Dept(ctx).Query().
		Where(dept.IDEQ(id), dept.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, org.ErrDeptNotFound
		}
		return nil, orgStoreError(ctx, r.logger, "org.dept.find_by_id", err)
	}
	return toBizDept(po), nil
}

func (r *deptRepo) FindByParentAndName(ctx context.Context, parentID int64, name string) (*orgentity.Dept, error) {
	po, err := r.data.DB.Dept(ctx).Query().
		Where(
			dept.ParentIDEQ(parentID),
			dept.DeptNameEQ(name),
			dept.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, org.ErrDeptNotFound
		}
		return nil, orgStoreError(ctx, r.logger, "org.dept.find_by_parent_and_name", err)
	}
	return toBizDept(po), nil
}

func (r *deptRepo) ListDepts(ctx context.Context) ([]*orgentity.Dept, error) {
	pos, err := r.data.DB.Dept(ctx).Query().
		Where(dept.DeletedAtIsNil()).
		Order(dept.ByParentID(), dept.ByOrderNum(), dept.ByID()).
		All(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.dept.list", err)
	}
	depts := make([]*orgentity.Dept, 0, len(pos))
	for _, po := range pos {
		depts = append(depts, toBizDept(po))
	}
	return depts, nil
}

// CreateDept inserts the department row. The caller owns the transaction.
func (r *deptRepo) CreateDept(ctx context.Context, d *orgentity.Dept) (*orgentity.Dept, error) {
	po, err := r.data.DB.Dept(ctx).Create().
		SetID(newSnowflakeID()).
		SetParentID(d.ParentID).
		SetDeptName(d.DeptName).
		SetOrderNum(d.OrderNum).
		SetLeader(d.Leader).
		SetPhone(d.Phone).
		SetEmail(d.Email).
		SetStatus(int8(d.Status)).
		SetCreateBy(d.CreateBy).
		SetUpdateBy(d.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.dept.create", err)
	}
	return toBizDept(po), nil
}

// UpdateDept applies the mutation and returns it with its pre-image — the
// enable/disable transition the ancestor cascade keys on. The caller owns the
// transaction.
func (r *deptRepo) UpdateDept(ctx context.Context, d *orgentity.Dept) (*orgentity.Dept, *orgentity.Dept, error) {
	current, err := r.data.DB.Dept(ctx).Query().
		Where(dept.IDEQ(d.ID), dept.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, org.ErrDeptNotFound
		}
		return nil, nil, orgStoreError(ctx, r.logger, "org.dept.update", err)
	}
	po, err := r.data.DB.Dept(ctx).UpdateOneID(d.ID).
		SetParentID(d.ParentID).
		SetDeptName(d.DeptName).
		SetOrderNum(d.OrderNum).
		SetLeader(d.Leader).
		SetPhone(d.Phone).
		SetEmail(d.Email).
		SetStatus(int8(d.Status)).
		SetUpdateBy(d.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, org.ErrDeptNotFound
		}
		return nil, nil, orgStoreError(ctx, r.logger, "org.dept.update", err)
	}
	return toBizDept(po), toBizDept(current), nil
}

// EnableDepts sets status=0 on the given department ids — the ancestor
// cascade behind enabling a department — stamping actorID. Missing ids are
// skipped: the cascade repairs a display state, it is not a security write.
// The caller owns the transaction.
func (r *deptRepo) EnableDepts(ctx context.Context, ids []int64, actorID int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := r.data.DB.Dept(ctx).Update().
		Where(dept.IDIn(ids...), dept.DeletedAtIsNil()).
		SetStatus(0).
		SetUpdateBy(actorID).
		Save(ctx); err != nil {
		return orgStoreError(ctx, r.logger, "org.dept.enable_depts", err)
	}
	return nil
}

// DeleteDept soft-deletes the department, stamping actorID (ADR 0002 — no row
// is hard-deleted). The caller owns the transaction.
func (r *deptRepo) DeleteDept(ctx context.Context, id int64, actorID int64) error {
	err := r.data.DB.Dept(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return org.ErrDeptNotFound
		}
		return orgStoreError(ctx, r.logger, "org.dept.delete", err)
	}
	return nil
}

func (r *deptRepo) CountChildDepts(ctx context.Context, parentID int64) (int, error) {
	n, err := r.data.DB.Dept(ctx).Query().
		Where(dept.ParentIDEQ(parentID), dept.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return 0, orgStoreError(ctx, r.logger, "org.dept.count_children", err)
	}
	return n, nil
}

// orgStoreError maps an unexpected storage failure behind the org repos
// (ADR 0007): the raw error is logged once at the handling site with its
// operation and returned as the module's sentinel, so no raw ent error
// crosses the repository boundary. Expected business conditions
// (ent.IsNotFound and friends) map silently at the call sites; a canceled
// request is benign and returns the sentinel without logging.
func orgStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return org.ErrOrgStoreUnavailable
	}
	logger.ErrorContext(ctx, "org store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return org.ErrOrgStoreUnavailable
}
