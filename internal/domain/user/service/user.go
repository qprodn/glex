// Package service holds the System User module's domain service: the only
// holder of the module's repository port (ADR 0005). Application use cases
// call this interface and never touch a repository directly; the Application
// service remains the transaction root, so every method joins the caller's
// ambient transaction. Cross-domain orchestration (transactions, credentials,
// session revocation) stays with the Application service.
package service

import (
	"context"
	"errors"
	"log/slog"

	"glex/internal/domain/user"
	"glex/internal/domain/user/entity"
	"glex/internal/domain/user/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// ListUsersRequest is the user-list read as the service states it: the
// shared query shape plus the department subtree filter the application
// resolved server-side. It flows application → service unchanged (ADR
// 0005/0025); the repo layer carries its own copy.
type ListUsersRequest struct {
	listq.Query
	DeptIDs []int64
}

// DeleteUserRequest is one security delete as the service states it: the
// System User to withdraw and the actor stamped into every audit field the
// write touches (ADR 0005/0025).
type DeleteUserRequest struct {
	ID      int64
	ActorID int64
}

// User is the System User domain service behind the user endpoints: the only
// path to the System User reads and writes (ADR 0005).
type User interface {
	Get(ctx context.Context, id int64) (*entity.User, error)
	List(ctx context.Context, req ListUsersRequest) ([]*entity.User, error)
	// Create validates uniqueness against active rows and persists the user.
	Create(ctx context.Context, u *entity.User) (*entity.User, error)
	// Update validates uniqueness (excluding self) and persists the change,
	// returning the write with its pre-image.
	Update(ctx context.Context, u *entity.User) (*entity.UserUpdate, error)
	// Delete soft-deletes the user and its Role Assignment, returning the
	// identities whose sessions must be revoked (always {req.ID}).
	Delete(ctx context.Context, req DeleteUserRequest) ([]int64, error)
	// HasUsersInDept reports whether any active user's department reference
	// points at deptID — the org module's department guards behind the
	// crossdomain user contract (ADR 0013).
	HasUsersInDept(ctx context.Context, deptID int64) (bool, error)
}

type userImpl struct {
	users  repository.UserRepo
	logger *slog.Logger
}

// NewUserService creates the System User domain service.
func NewUserService(users repository.UserRepo, logger *slog.Logger) User {
	return &userImpl{
		users:  users,
		logger: logger.With(slog.String("component", "domain.user.svc")),
	}
}

func (s *userImpl) Get(ctx context.Context, id int64) (*entity.User, error) {
	u, err := s.users.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.get", err)
		return nil, err
	}
	return u, nil
}

func (s *userImpl) List(ctx context.Context, req ListUsersRequest) ([]*entity.User, error) {
	users, err := s.users.ListUsers(ctx, repository.ListUsersRequest{Query: req.Query, DeptIDs: req.DeptIDs})
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.list", err)
		return nil, err
	}
	return users, nil
}

func (s *userImpl) Create(ctx context.Context, u *entity.User) (*entity.User, error) {
	if err := assertUnique(ctx, s.users, u, 0); err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.create", err)
		return nil, err
	}
	created, err := s.users.CreateUser(ctx, u)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.create", err)
		return nil, err
	}
	return created, nil
}

func (s *userImpl) Update(ctx context.Context, u *entity.User) (*entity.UserUpdate, error) {
	if err := assertUnique(ctx, s.users, u, u.ID); err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.update", err)
		return nil, err
	}
	updated, err := s.users.UpdateUser(ctx, u)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.update", err)
		return nil, err
	}
	return updated, nil
}

func (s *userImpl) Delete(ctx context.Context, req DeleteUserRequest) ([]int64, error) {
	revoked, err := s.users.DeleteUser(ctx, repository.DeleteUserRequest{ID: req.ID, ActorID: req.ActorID})
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.delete", err)
		return nil, err
	}
	return revoked, nil
}

func (s *userImpl) HasUsersInDept(ctx context.Context, deptID int64) (bool, error) {
	n, err := s.users.CountByDeptID(ctx, deptID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.svc.has_users_in_dept", err)
		return false, err
	}
	return n > 0, nil
}

// assertUnique checks user_name (always) and email/phone_number (when set)
// are not already taken, ignoring the record with excludeID (0 = none). A
// not-found from the repo is the expected "available" case. Checks run
// against active rows only, so soft-deleted values are reusable. Its
// caller's method boundary owns the breadcrumb (ADR 0011). A package-level
// invariant helper, unexported rather than conformed (ADR 0025).
func assertUnique(ctx context.Context, repo repository.UserRepo, u *entity.User, excludeID int64) error {
	if existing, err := repo.FindByUserName(ctx, u.UserName); err != nil {
		if !errors.Is(err, user.ErrUserNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return user.ErrUserNameConflict
	}
	if u.Email != "" {
		if existing, err := repo.FindByEmail(ctx, u.Email); err != nil {
			if !errors.Is(err, user.ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return user.ErrEmailConflict
		}
	}
	if u.PhoneNumber != "" {
		if existing, err := repo.FindByPhoneNumber(ctx, u.PhoneNumber); err != nil {
			if !errors.Is(err, user.ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return user.ErrPhoneConflict
		}
	}
	return nil
}
