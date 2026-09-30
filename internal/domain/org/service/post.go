package service

import (
	"context"
	"errors"
	"log/slog"

	"glex/internal/domain/org"
	"glex/internal/domain/org/entity"
	"glex/internal/domain/org/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// Post is the org domain service behind the post endpoints: the only path to
// the Post reads and writes (ADR 0005). Posts are department-scoped
// (ADR 0015): the name is unique within its department, the code is unique
// across all posts. Methods join the caller's ambient transaction; the
// Application service owns the transaction boundary. Every error a method
// returns to its caller first logs its Warn breadcrumb (ADR 0011).
// ListPostsRequest is the post-list read as the service states it: the
// shared query shape plus the department subtree filter the application
// resolved server-side (ADR 0005/0025).
type ListPostsRequest struct {
	listq.Query
	DeptIDs []int64
}

// DeletePostRequest identifies the post a Delete withdraws and the actor
// whose id lands in the audit fields (ADR 0005/0025).
type DeletePostRequest struct {
	ID      int64
	ActorID int64
}

type Post interface {
	Get(ctx context.Context, id int64) (*entity.Post, error)
	List(ctx context.Context, req ListPostsRequest) ([]*entity.Post, error)
	// Create validates the post (shape, department exists, name per dept and
	// code globally unique) and persists it.
	Create(ctx context.Context, p *entity.Post) (*entity.Post, error)
	// Update validates the change (shape, department exists, uniqueness,
	// disable guard) and persists it.
	Update(ctx context.Context, p *entity.Post) (*entity.Post, error)
	// Delete soft-deletes the post identified by req, refusing while users
	// are still assigned.
	Delete(ctx context.Context, req DeletePostRequest) error
}

type postImpl struct {
	posts     repository.PostRepo
	depts     repository.DeptRepo
	userPosts repository.UserPostRepo
	logger    *slog.Logger
}

// NewPostService creates the post domain service. depts backs the
// department-existence check; userPosts backs the "a post with users cannot
// be deleted or disabled" guard.
func NewPostService(posts repository.PostRepo, depts repository.DeptRepo, userPosts repository.UserPostRepo, logger *slog.Logger) Post {
	return &postImpl{
		posts:     posts,
		depts:     depts,
		userPosts: userPosts,
		logger:    logger.With(slog.String("component", "domain.org.svc")),
	}
}

func (s *postImpl) Get(ctx context.Context, id int64) (*entity.Post, error) {
	p, err := s.posts.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.get_post", err)
		return nil, err
	}
	return p, nil
}

func (s *postImpl) List(ctx context.Context, req ListPostsRequest) ([]*entity.Post, error) {
	ps, err := s.posts.ListPosts(ctx, repository.ListPostsRequest{Query: req.Query, DeptIDs: req.DeptIDs})
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.list_posts", err)
		return nil, err
	}
	return ps, nil
}

func (s *postImpl) Create(ctx context.Context, p *entity.Post) (*entity.Post, error) {
	if err := p.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_post", err)
		return nil, err
	}
	// A post hangs off a department; a dangling dept_id is refused (RuoYi
	// checks only not-null — glex keeps the reference honest). Whether the
	// department is enabled is not checked, matching RuoYi.
	if _, err := s.depts.FindByID(ctx, p.DeptID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_post", err)
		return nil, err
	}
	if err := assertPostUnique(ctx, s.posts, p, 0); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_post", err)
		return nil, err
	}
	created, err := s.posts.CreatePost(ctx, p)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_post", err)
		return nil, err
	}
	return created, nil
}

func (s *postImpl) Update(ctx context.Context, p *entity.Post) (*entity.Post, error) {
	if err := p.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
		return nil, err
	}
	current, err := s.posts.FindByID(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
		return nil, err
	}
	// The department reference is validated on every update, not only on a
	// move: a pre-existing dangling dept_id (from a manual write) heals here
	// instead of surviving re-saves.
	if _, err := s.depts.FindByID(ctx, p.DeptID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
		return nil, err
	}
	if err := assertPostUnique(ctx, s.posts, p, p.ID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
		return nil, err
	}
	// Disabling is refused while users are still assigned (RuoYi: 该岗位下
	// 存在已分配用户，不能禁用); the guard fires on the enabled→disabled
	// transition, so re-saving a disabled post never re-trips it.
	if current.Enabled() && !p.Enabled() {
		n, err := s.userPosts.CountUserPosts(ctx, p.ID)
		if err != nil {
			logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
			return nil, err
		}
		if n > 0 {
			err := org.ErrPostInUse
			logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
			return nil, err
		}
	}
	result, err := s.posts.UpdatePost(ctx, p)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_post", err)
		return nil, err
	}
	return result.Post, nil
}

func (s *postImpl) Delete(ctx context.Context, req DeletePostRequest) error {
	if _, err := s.posts.FindByID(ctx, req.ID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_post", err)
		return err
	}
	n, err := s.userPosts.CountUserPosts(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_post", err)
		return err
	}
	if n > 0 {
		err := org.ErrPostInUse
		logx.Propagate(ctx, s.logger, "org.svc.delete_post", err)
		return err
	}
	if err := s.posts.DeletePost(ctx, repository.DeletePostRequest{ID: req.ID, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_post", err)
		return err
	}
	return nil
}

// assertPostUnique checks post_name is free within the post's department and
// post_code is free globally, ignoring the record with excludeID (0 = none).
// Not-found from either lookup is the expected "available" case; checks run
// against active rows only, so soft-deleted values are reusable. Its
// caller's method boundary owns the breadcrumb (ADR 0011). A package-level
// invariant helper, unexported rather than conformed (ADR 0025).
func assertPostUnique(ctx context.Context, repo repository.PostRepo, p *entity.Post, excludeID int64) error {
	if existing, err := repo.FindByDeptAndName(ctx, repository.FindByDeptAndNameRequest{DeptID: p.DeptID, Name: p.PostName}); err != nil {
		if !errors.Is(err, org.ErrPostNotFound) {
			return err
		}
	} else if existing.ID != excludeID {
		return org.ErrPostNameTaken
	}
	if existing, err := repo.FindByPostCode(ctx, p.PostCode); err != nil {
		if !errors.Is(err, org.ErrPostNotFound) {
			return err
		}
	} else if existing.ID != excludeID {
		return org.ErrPostCodeTaken
	}
	return nil
}
