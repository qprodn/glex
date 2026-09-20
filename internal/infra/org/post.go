package orginfra

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/org"
	orgentity "glex/internal/domain/org/entity"
	orgrepo "glex/internal/domain/org/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/post"
	"glex/internal/infra/store"
	"glex/pkg/listq"
)

// toBizPost maps an ent Post PO to a orgentity.Post DO.
func toBizPost(po *ent.Post) *orgentity.Post {
	return &orgentity.Post{
		ID:        po.ID,
		DeptID:    po.DeptID,
		PostCode:  po.PostCode,
		PostName:  po.PostName,
		PostSort:  po.PostSort,
		Status:    int32(po.Status),
		Remark:    po.Remark,
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
	}
}

type postRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewPostRepo creates a new PostRepo instance.
func NewPostRepo(data *store.Data) orgrepo.PostRepo {
	return &postRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.org.post")),
	}
}

func (r *postRepo) FindByID(ctx context.Context, id int64) (*orgentity.Post, error) {
	po, err := r.data.DB.Post(ctx).Query().
		Where(post.IDEQ(id), post.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, org.ErrPostNotFound
		}
		return nil, orgStoreError(ctx, r.logger, "org.post.find_by_id", err)
	}
	return toBizPost(po), nil
}

func (r *postRepo) FindByDeptAndName(ctx context.Context, deptID int64, name string) (*orgentity.Post, error) {
	po, err := r.data.DB.Post(ctx).Query().
		Where(
			post.DeptIDEQ(deptID),
			post.PostNameEQ(name),
			post.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, org.ErrPostNotFound
		}
		return nil, orgStoreError(ctx, r.logger, "org.post.find_by_dept_and_name", err)
	}
	return toBizPost(po), nil
}

func (r *postRepo) FindByPostCode(ctx context.Context, code string) (*orgentity.Post, error) {
	po, err := r.data.DB.Post(ctx).Query().
		Where(post.PostCodeEQ(code), post.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, org.ErrPostNotFound
		}
		return nil, orgStoreError(ctx, r.logger, "org.post.find_by_code", err)
	}
	return toBizPost(po), nil
}

func (r *postRepo) ListPosts(ctx context.Context, q listq.Query, deptIDs []int64) ([]*orgentity.Post, error) {
	query := r.data.DB.Post(ctx).Query().
		Where(
			post.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		)
	if len(deptIDs) > 0 {
		query = query.Where(post.DeptIDIn(deptIDs...))
	}
	pos, err := query.
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.post.list", err)
	}
	posts := make([]*orgentity.Post, 0, len(pos))
	for _, po := range pos {
		posts = append(posts, toBizPost(po))
	}
	return posts, nil
}

func (r *postRepo) ListByIDs(ctx context.Context, ids []int64) ([]*orgentity.Post, error) {
	pos, err := r.data.DB.Post(ctx).Query().
		Where(post.IDIn(ids...), post.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.post.list_by_ids", err)
	}
	posts := make([]*orgentity.Post, 0, len(pos))
	for _, po := range pos {
		posts = append(posts, toBizPost(po))
	}
	return posts, nil
}

func (r *postRepo) ListActiveByDept(ctx context.Context, deptID int64) ([]*orgentity.Post, error) {
	pos, err := r.data.DB.Post(ctx).Query().
		Where(
			post.DeptIDEQ(deptID),
			post.StatusEQ(0),
			post.DeletedAtIsNil(),
		).
		Order(post.ByPostSort(), post.ByID()).
		All(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.post.list_active_by_dept", err)
	}
	posts := make([]*orgentity.Post, 0, len(pos))
	for _, po := range pos {
		posts = append(posts, toBizPost(po))
	}
	return posts, nil
}

// CreatePost inserts the post row. The caller owns the transaction.
func (r *postRepo) CreatePost(ctx context.Context, p *orgentity.Post) (*orgentity.Post, error) {
	po, err := r.data.DB.Post(ctx).Create().
		SetID(newSnowflakeID()).
		SetDeptID(p.DeptID).
		SetPostCode(p.PostCode).
		SetPostName(p.PostName).
		SetPostSort(p.PostSort).
		SetStatus(int8(p.Status)).
		SetRemark(p.Remark).
		SetCreateBy(p.CreateBy).
		SetUpdateBy(p.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.post.create", err)
	}
	return toBizPost(po), nil
}

// UpdatePost applies the mutation and returns it with its pre-image — the
// enable/disable transition the in-use guard keys on. The caller owns the
// transaction.
func (r *postRepo) UpdatePost(ctx context.Context, p *orgentity.Post) (*orgentity.Post, *orgentity.Post, error) {
	current, err := r.data.DB.Post(ctx).Query().
		Where(post.IDEQ(p.ID), post.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, org.ErrPostNotFound
		}
		return nil, nil, orgStoreError(ctx, r.logger, "org.post.update", err)
	}
	po, err := r.data.DB.Post(ctx).UpdateOneID(p.ID).
		SetDeptID(p.DeptID).
		SetPostCode(p.PostCode).
		SetPostName(p.PostName).
		SetPostSort(p.PostSort).
		SetStatus(int8(p.Status)).
		SetRemark(p.Remark).
		SetUpdateBy(p.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, org.ErrPostNotFound
		}
		return nil, nil, orgStoreError(ctx, r.logger, "org.post.update", err)
	}
	return toBizPost(po), toBizPost(current), nil
}

// DeletePost soft-deletes the post, stamping actorID (ADR 0002 — no row is
// hard-deleted). The caller owns the transaction.
func (r *postRepo) DeletePost(ctx context.Context, id int64, actorID int64) error {
	err := r.data.DB.Post(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return org.ErrPostNotFound
		}
		return orgStoreError(ctx, r.logger, "org.post.delete", err)
	}
	return nil
}

func (r *postRepo) CountByDept(ctx context.Context, deptID int64) (int, error) {
	n, err := r.data.DB.Post(ctx).Query().
		Where(post.DeptIDEQ(deptID), post.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return 0, orgStoreError(ctx, r.logger, "org.post.count_by_dept", err)
	}
	return n, nil
}
