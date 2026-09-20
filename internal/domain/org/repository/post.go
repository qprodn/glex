package repository

import (
	"context"

	"glex/internal/domain/org/entity"
	"glex/pkg/listq"
)

// PostRepo is the repository interface for posts (the inversion seam
// declared in domain, implemented in data).
type PostRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Post, error)
	// FindByDeptAndName returns the post named name within deptID — the
	// per-department uniqueness lookup. ErrPostNotFound means "available".
	FindByDeptAndName(ctx context.Context, deptID int64, name string) (*entity.Post, error)
	// FindByPostCode returns the post coded code — the global uniqueness
	// lookup. ErrPostNotFound means "available".
	FindByPostCode(ctx context.Context, code string) (*entity.Post, error)
	// ListPosts lists posts matching the options (filter/order/offset/limit
	// plus the department-subtree restriction).
	ListPosts(ctx context.Context, q listq.Query, deptIDs []int64) ([]*entity.Post, error)
	// ListByIDs returns the active posts bearing the given ids (soft-deleted
	// excluded) — the assignment-validation read. Order follows the ids.
	ListByIDs(ctx context.Context, ids []int64) ([]*entity.Post, error)
	// ListActiveByDept returns the department's enabled posts, ordered by
	// (post_sort, id) — the assignment dialog's candidate set.
	ListActiveByDept(ctx context.Context, deptID int64) ([]*entity.Post, error)
	// CreatePost persists the post. The caller owns the transaction.
	CreatePost(ctx context.Context, p *entity.Post) (*entity.Post, error)
	// UpdatePost persists the mutation and returns it with its pre-image
	// (the enable/disable transition the in-use guard keys on). The caller
	// owns the transaction.
	UpdatePost(ctx context.Context, p *entity.Post) (*entity.Post, *entity.Post, error)
	// DeletePost soft-deletes the post, stamping actorID (ADR 0002 — no row
	// is hard-deleted). The caller owns the transaction.
	DeletePost(ctx context.Context, id int64, actorID int64) error
	// CountByDept counts the department's non-withdrawn posts — the dept
	// delete guard (RuoYi 6.0: 部门存在岗位).
	CountByDept(ctx context.Context, deptID int64) (int, error)
}
