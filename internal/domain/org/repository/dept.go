package repository

import (
	"context"

	"glex/internal/domain/org/entity"
)

// DeptRepo is the repository interface for departments (the inversion seam
// declared in domain, implemented in data).
type DeptRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Dept, error)
	// FindByParentAndName returns the department named name under parentID —
	// the sibling-uniqueness lookup. ErrDeptNotFound means "available".
	FindByParentAndName(ctx context.Context, parentID int64, name string) (*entity.Dept, error)
	// ListDepts returns every non-withdrawn department — enabled and disabled
	// alike (status is business state, withdrawal is deleted_at) — ordered by
	// (parent_id, order_num, id), the input BuildDeptTree assembles.
	ListDepts(ctx context.Context) ([]*entity.Dept, error)
	// CreateDept persists the department. The caller owns the transaction.
	CreateDept(ctx context.Context, d *entity.Dept) (*entity.Dept, error)
	// UpdateDept persists the mutation and returns it with its pre-image (the
	// enable/disable transition the ancestor cascade keys on). The caller
	// owns the transaction.
	UpdateDept(ctx context.Context, d *entity.Dept) (*entity.Dept, *entity.Dept, error)
	// EnableDepts sets status=0 on the given department ids — the ancestor
	// cascade behind enabling a department — stamping actorID. The caller
	// owns the transaction.
	EnableDepts(ctx context.Context, ids []int64, actorID int64) error
	// DeleteDept soft-deletes the department, stamping actorID (ADR 0002 —
	// no row is hard-deleted). The caller owns the transaction.
	DeleteDept(ctx context.Context, id int64, actorID int64) error
	// CountChildDepts counts the non-withdrawn departments directly under
	// parentID — the delete guard (RuoYi: 存在下级部门,不允许删除).
	CountChildDepts(ctx context.Context, parentID int64) (int, error)
}
