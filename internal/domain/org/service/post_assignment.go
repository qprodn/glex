package service

import (
	"context"
	"log/slog"

	"glex/internal/domain/org"
	"glex/internal/domain/org/entity"
	"glex/internal/domain/org/repository"
	"glex/pkg/logx"
)

// ListForUserRequest names the System User whose assignment dialog is read
// and the form's not-yet-saved department choice (0 = none — the user's
// stored department supplies the candidates).
type ListForUserRequest struct {
	UserID       int64
	DeptOverride int64
}

// ReplaceUserPostsRequest is one full replacement of a user's Post
// Assignment as the service states it: the user, the post ids to store, and
// the actor stamped into the audit fields (ADR 0005/0025).
type ReplaceUserPostsRequest struct {
	UserID  int64
	PostIDs []int64
	ActorID int64
}

// PostAssignment is the org domain service behind the user↔post link
// endpoints (岗位分配): the only path to the link reads and writes
// (ADR 0005). Assigning is full-replacement, the Role Assignment semantics.
// Methods join the caller's ambient transaction; the Application service
// owns the transaction boundary. Every error a method returns to its caller
// first logs its Warn breadcrumb (ADR 0011).
type PostAssignment interface {
	// ListForUser returns the effective department's active posts with
	// whether the user is assigned each — the post dialog's data in one
	// call. req.DeptOverride (0 = none) is the form's not-yet-saved
	// department choice; absent it, the user's stored department supplies
	// the candidates. Gated on req.UserID's user existing.
	ListForUser(ctx context.Context, req ListForUserRequest) ([]*entity.PostAssignment, error)
	// ReplaceForUser validates and dedupes req.PostIDs (every id must exist
	// and be enabled), replaces the user's Post Assignment in full (empty =
	// clear all), and returns the freshly-listed assignment.
	ReplaceForUser(ctx context.Context, req ReplaceUserPostsRequest) ([]*entity.PostAssignment, error)
}

type postAssignmentImpl struct {
	posts     repository.PostRepo
	userPosts repository.UserPostRepo
	logger    *slog.Logger
}

// NewPostAssignmentService creates the Post Assignment domain service.
// userPosts owns the sys_user_post rows and the by-user existence gate;
// posts backs the assignment validation and the candidate listing.
func NewPostAssignmentService(posts repository.PostRepo, userPosts repository.UserPostRepo, logger *slog.Logger) PostAssignment {
	return &postAssignmentImpl{
		posts:     posts,
		userPosts: userPosts,
		logger:    logger.With(slog.String("component", "domain.org.svc")),
	}
}

func (s *postAssignmentImpl) ListForUser(ctx context.Context, req ListForUserRequest) ([]*entity.PostAssignment, error) {
	deptID, err := s.userPosts.UserDeptID(ctx, req.UserID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.list_post_assignments", err)
		return nil, err
	}
	if req.DeptOverride != 0 {
		deptID = req.DeptOverride
	}
	assigned := make(map[int64]struct{})
	if ids, err := s.userPosts.AssignedPostIDs(ctx, req.UserID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.list_post_assignments", err)
		return nil, err
	} else {
		for _, id := range ids {
			assigned[id] = struct{}{}
		}
	}
	candidates, err := s.posts.ListActiveByDept(ctx, deptID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.list_post_assignments", err)
		return nil, err
	}
	out := make([]*entity.PostAssignment, 0, len(candidates))
	for _, p := range candidates {
		_, ok := assigned[p.ID]
		out = append(out, &entity.PostAssignment{Post: p, Assigned: ok})
	}
	return out, nil
}

func (s *postAssignmentImpl) ReplaceForUser(ctx context.Context, req ReplaceUserPostsRequest) ([]*entity.PostAssignment, error) {
	if err := assertUserExists(ctx, s.userPosts, req.UserID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.replace_post_assignments", err)
		return nil, err
	}
	unique, err := assertPostsAssignable(ctx, s.posts, req.PostIDs)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.replace_post_assignments", err)
		return nil, err
	}
	if err := s.userPosts.ReplaceUserPosts(ctx, repository.ReplaceUserPostsRequest{
		UserID:  req.UserID,
		PostIDs: unique,
		ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.replace_post_assignments", err)
		return nil, err
	}
	return s.ListForUser(ctx, ListForUserRequest{UserID: req.UserID})
}

// assertUserExists gates the by-user assignment endpoints on the System User
// existing (not soft-deleted). Its caller's method boundary owns the
// breadcrumb (ADR 0011). A package-level invariant helper, unexported rather
// than conformed (ADR 0025).
func assertUserExists(ctx context.Context, repo repository.UserPostRepo, userID int64) error {
	exists, err := repo.UserExists(ctx, userID)
	if err != nil {
		return err
	}
	if !exists {
		return org.ErrUserNotFound
	}
	return nil
}

// assertPostsAssignable dedupes postIDs and checks every id exists, is not
// soft-deleted, and is enabled (the repo's ListByIDs filters deleted rows;
// enabled is checked here). An empty list is legal — it means "no posts" —
// and passes without touching the repo. Its caller's method boundary owns
// the breadcrumb (ADR 0011). A package-level invariant helper, unexported
// rather than conformed (ADR 0025).
func assertPostsAssignable(ctx context.Context, postRepo repository.PostRepo, postIDs []int64) ([]int64, error) {
	unique := dedupeIDs(postIDs)
	if len(unique) == 0 {
		return unique, nil
	}
	posts, err := postRepo.ListByIDs(ctx, unique)
	if err != nil {
		return nil, err
	}
	if len(posts) != len(unique) {
		return nil, org.ErrPostNotAssignable
	}
	for _, p := range posts {
		if !p.Enabled() {
			return nil, org.ErrPostNotAssignable
		}
	}
	return unique, nil
}

// dedupeIDs removes duplicates, preserving first-occurrence order.
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
