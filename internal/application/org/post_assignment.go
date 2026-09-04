package org

import (
	"context"
	"log/slog"

	orgv1 "glex/api/org/v1"
	"glex/internal/bizpkg"
	orgentity "glex/internal/domain/org/entity"
	orgservice "glex/internal/domain/org/service"
	"glex/pkg/logx"
)

// PostAssignmentApplication implements the user↔post link service (岗位分配):
// the two endpoints behind the user editor's post dialog. All repository
// access goes through the Post Assignment domain service (ADR 0005).
type PostAssignmentApplication struct {
	orgv1.UnimplementedPostAssignmentServiceServer

	svc    orgservice.PostAssignment
	tx     Transaction
	logger *slog.Logger
}

// NewPostAssignmentApplication creates a PostAssignmentApplication. svc is
// the Post Assignment domain service — the module's only holder of the
// sys_user_post rows; tx wraps the replacement write.
func NewPostAssignmentApplication(svc orgservice.PostAssignment, tx Transaction, logger *slog.Logger) *PostAssignmentApplication {
	return &PostAssignmentApplication{
		svc:    svc,
		tx:     tx,
		logger: logger.With(slog.String("component", "application.org.post_assignment")),
	}
}

// ListUserPosts returns the effective department's active posts with their
// assigned flags — the post dialog's data in one call. The optional dept_id
// override follows a not-yet-saved department change in the form; absent it,
// the user's stored department supplies the candidates.
func (s *PostAssignmentApplication) ListUserPosts(ctx context.Context, req *orgv1.ListUserPostsRequest) (*orgv1.UserPosts, error) {
	items, err := s.svc.ListForUser(ctx, req.GetUserId(), req.GetDeptId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.list_user_posts", err)
		return nil, err
	}
	return convertUserPosts(items), nil
}

// UpdateUserPosts replaces the user's Post Assignment in one transaction
// (full replacement; empty post_ids clears all). Unknown, soft-deleted, or
// disabled post ids are rejected with POST_NOT_ASSIGNABLE; duplicates are
// silently deduped. The response is the freshly-listed assignment.
func (s *PostAssignmentApplication) UpdateUserPosts(ctx context.Context, req *orgv1.UpdateUserPostsRequest) (*orgv1.UserPosts, error) {
	var items []*orgentity.PostAssignment
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		items, err = s.svc.ReplaceForUser(ctx, req.GetUserId(), req.GetPostIds(), bizpkg.AuditActor(ctx))
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.update_user_posts", err)
		return nil, err
	}
	return convertUserPosts(items), nil
}

// convertUserPosts maps the entity assignment list to the proto response.
func convertUserPosts(items []*orgentity.PostAssignment) *orgv1.UserPosts {
	out := &orgv1.UserPosts{Items: make([]*orgv1.PostAssignment, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, &orgv1.PostAssignment{
			Post:     convertPost(item.Post),
			Assigned: item.Assigned,
		})
	}
	return out
}
