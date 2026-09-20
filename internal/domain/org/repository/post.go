package repository

import (
	"context"

	"glex/internal/domain/org/entity"
	"glex/pkg/listq"
)

// ListPostsRequest is one post list read: the shared query shape plus the
// department-subtree restriction the application resolves through the
// department domain service riding the domain List down as DeptIDs — empty
// filters nothing. Like the Query it carries, the request flows
// application → domain → repo unchanged.
type ListPostsRequest struct {
	listq.Query
	DeptIDs []int64
}

// FindByDeptAndNameRequest names the (dept_id, post_name) pair resolved
// among non-withdrawn posts.
type FindByDeptAndNameRequest struct {
	DeptID int64
	Name   string
}

// DeletePostRequest identifies the post a Delete withdraws and the actor
// whose id lands in the audit fields.
type DeletePostRequest struct {
	ID      int64
	ActorID int64
}

// PostRepo is the repository interface for posts (the inversion seam
// declared in domain, implemented in data).
type PostRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Post, error)
	// FindByDeptAndName returns the post named req.Name within req.DeptID —
	// the per-department uniqueness lookup. ErrPostNotFound means "available".
	FindByDeptAndName(ctx context.Context, req FindByDeptAndNameRequest) (*entity.Post, error)
	// FindByPostCode returns the post coded code — the global uniqueness
	// lookup. ErrPostNotFound means "available".
	FindByPostCode(ctx context.Context, code string) (*entity.Post, error)
	// ListPosts lists posts matching the request (filter/order/offset/limit
	// plus the department-subtree restriction).
	ListPosts(ctx context.Context, req ListPostsRequest) ([]*entity.Post, error)
	// ListByIDs returns the active posts bearing the given ids (soft-deleted
	// excluded) — the assignment-validation read. Order follows the ids.
	ListByIDs(ctx context.Context, ids []int64) ([]*entity.Post, error)
	// ListActiveByDept returns the department's enabled posts, ordered by
	// (post_sort, id) — the assignment dialog's candidate set.
	ListActiveByDept(ctx context.Context, deptID int64) ([]*entity.Post, error)
	// CreatePost persists the post. The caller owns the transaction.
	CreatePost(ctx context.Context, p *entity.Post) (*entity.Post, error)
	// UpdatePost persists the mutation and returns the write paired with its
	// pre-image (the enable/disable transition the in-use guard keys on). The
	// caller owns the transaction.
	UpdatePost(ctx context.Context, p *entity.Post) (*entity.PostUpdate, error)
	// DeletePost soft-deletes the post identified by req, stamping req.ActorID
	// (ADR 0002 — no row is hard-deleted). The caller owns the transaction.
	DeletePost(ctx context.Context, req DeletePostRequest) error
	// CountByDept counts the department's non-withdrawn posts — the dept
	// delete guard (RuoYi 6.0: 部门存在岗位).
	CountByDept(ctx context.Context, deptID int64) (int, error)
}
