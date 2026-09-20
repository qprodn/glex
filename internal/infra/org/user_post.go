package orginfra

import (
	"context"
	"log/slog"
	"time"

	"glex/internal/domain/org"
	orgrepo "glex/internal/domain/org/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/post"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/ent/userpost"
	"glex/internal/infra/store"
)

// userPostRepo implements org's UserPostRepo over sys_user_post. It reads
// sys_user directly (existence and dept reference) through the shared ent
// client — the access module's sys_user_role precedent: the link owner may
// traverse into the linked user rows, it owns no user domain logic.
type userPostRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewUserPostRepo creates a new UserPostRepo instance.
func NewUserPostRepo(data *store.Data) orgrepo.UserPostRepo {
	return &userPostRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.org.user_post")),
	}
}

func (r *userPostRepo) UserExists(ctx context.Context, userID int64) (bool, error) {
	exists, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(userID), user.DeletedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return false, orgStoreError(ctx, r.logger, "org.user_post.user_exists", err)
	}
	return exists, nil
}

func (r *userPostRepo) UserDeptID(ctx context.Context, userID int64) (int64, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(userID), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, org.ErrUserNotFound
		}
		return 0, orgStoreError(ctx, r.logger, "org.user_post.user_dept_id", err)
	}
	return po.DeptID, nil
}

func (r *userPostRepo) AssignedPostIDs(ctx context.Context, userID int64) ([]int64, error) {
	ids, err := r.data.DB.UserPost(ctx).Query().
		Where(userpost.UserIDEQ(userID), userpost.DeletedAtIsNil()).
		Order(userpost.ByPostID()).
		IDs(ctx)
	if err != nil {
		return nil, orgStoreError(ctx, r.logger, "org.user_post.assigned_post_ids", err)
	}
	return ids, nil
}

// ReplaceUserPosts atomically replaces the user's active Post Assignment:
// the current link rows are soft-deleted (stamping req.ActorID), then the new
// generation is inserted (ADR 0002 — full replacement of the active set; a
// re-assigned pair is a new row). The caller owns the transaction.
func (r *userPostRepo) ReplaceUserPosts(ctx context.Context, req orgrepo.ReplaceUserPostsRequest) error {
	if _, err := r.data.DB.UserPost(ctx).Update().
		Where(userpost.UserIDEQ(req.UserID), userpost.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Save(ctx); err != nil {
		return orgStoreError(ctx, r.logger, "org.user_post.replace", err)
	}
	if len(req.PostIDs) == 0 {
		return nil
	}
	builders := make([]*ent.UserPostCreate, 0, len(req.PostIDs))
	for _, postID := range req.PostIDs {
		builders = append(builders, r.data.DB.UserPost(ctx).Create().
			SetID(newSnowflakeID()).
			SetUserID(req.UserID).
			SetPostID(postID).
			SetCreateBy(req.ActorID).
			SetUpdateBy(req.ActorID))
	}
	if err := r.data.DB.UserPost(ctx).CreateBulk(builders...).Exec(ctx); err != nil {
		return orgStoreError(ctx, r.logger, "org.user_post.replace", err)
	}
	return nil
}

func (r *userPostRepo) CountUserPosts(ctx context.Context, postID int64) (int, error) {
	// Count active links whose user is not soft-deleted: a withdrawn user's
	// links are soft-deleted with them and never hold a post hostage.
	n, err := r.data.DB.Post(ctx).Query().
		Where(post.IDEQ(postID), post.DeletedAtIsNil()).
		QueryUserLinks().Where(userpost.DeletedAtIsNil()).
		QueryUser().Where(user.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return 0, orgStoreError(ctx, r.logger, "org.user_post.count_user_posts", err)
	}
	return n, nil
}
