package repository

import (
	"context"

	"glex/internal/domain/org/entity"
)

// FindByParentAndNameRequest names the (parent_id, dept_name) pair resolved
// among non-withdrawn departments.
type FindByParentAndNameRequest struct {
	ParentID int64
	Name     string
}

// EnableDeptsRequest is one ancestor cascade: the department ids brought
// back to enabled and the actor whose id lands in the audit fields.
type EnableDeptsRequest struct {
	IDs     []int64
	ActorID int64
}

// DeleteDeptRequest identifies the department a Delete withdraws and the
// actor whose id lands in the audit fields.
type DeleteDeptRequest struct {
	ID      int64
	ActorID int64
}

// DeptRepo is the repository interface for departments (the inversion seam
// declared in domain, implemented in data).
type DeptRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Dept, error)
	// FindByParentAndName returns the department named req.Name under
	// req.ParentID — the sibling-uniqueness lookup. ErrDeptNotFound means
	// "available".
	FindByParentAndName(ctx context.Context, req FindByParentAndNameRequest) (*entity.Dept, error)
	// ListDepts returns every non-withdrawn department — enabled and disabled
	// alike (status is business state, withdrawal is deleted_at) — ordered by
	// (parent_id, order_num, id), the input BuildDeptTree assembles.
	ListDepts(ctx context.Context) ([]*entity.Dept, error)
	// CreateDept persists the department. The caller owns the transaction.
	CreateDept(ctx context.Context, d *entity.Dept) (*entity.Dept, error)
	// UpdateDept persists the mutation and returns the write paired with its
	// pre-image (the enable/disable transition the ancestor cascade keys on).
	// The caller owns the transaction.
	UpdateDept(ctx context.Context, d *entity.Dept) (*entity.DeptUpdate, error)
	// EnableDepts sets status=0 on req.IDs — the ancestor cascade behind
	// enabling a department — stamping req.ActorID. The caller owns the
	// transaction.
	EnableDepts(ctx context.Context, req EnableDeptsRequest) error
	// DeleteDept soft-deletes the department identified by req, stamping
	// req.ActorID (ADR 0002 — no row is hard-deleted). The caller owns the
	// transaction.
	DeleteDept(ctx context.Context, req DeleteDeptRequest) error
	// CountChildDepts counts the non-withdrawn departments directly under
	// parentID — the delete guard (RuoYi: 存在下级部门,不允许删除).
	CountChildDepts(ctx context.Context, parentID int64) (int, error)
}
