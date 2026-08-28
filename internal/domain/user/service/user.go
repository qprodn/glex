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

	"glex/internal/domain/user"
	"glex/internal/domain/user/entity"
	"glex/internal/domain/user/repository"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// User is the System User domain service behind the user endpoints: the only
// path to the System User reads and writes (ADR 0005).
type User interface {
	Get(ctx context.Context, id int64) (*entity.User, error)
	List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.User, error)
	// Create validates uniqueness against active rows and persists the user.
	Create(ctx context.Context, u *entity.User) (*entity.User, error)
	// Update validates uniqueness (excluding self) and persists the change,
	// returning the write with its pre-image.
	Update(ctx context.Context, u *entity.User) (*entity.UserUpdate, error)
	// Delete soft-deletes the user and its Role Assignment, returning the
	// identities whose sessions must be revoked (always {id}).
	Delete(ctx context.Context, id, actorID int64) ([]int64, error)
}

type userImpl struct {
	users repository.UserRepo
}

// NewUserService creates the System User domain service.
func NewUserService(users repository.UserRepo) User {
	return &userImpl{users: users}
}

func (s *userImpl) Get(ctx context.Context, id int64) (*entity.User, error) {
	return s.users.FindByID(ctx, id)
}

func (s *userImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.User, error) {
	return s.users.ListUsers(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
}

func (s *userImpl) Create(ctx context.Context, u *entity.User) (*entity.User, error) {
	if err := AssertUnique(ctx, s.users, u, 0); err != nil {
		return nil, err
	}
	return s.users.CreateUser(ctx, u)
}

func (s *userImpl) Update(ctx context.Context, u *entity.User) (*entity.UserUpdate, error) {
	if err := AssertUnique(ctx, s.users, u, u.ID); err != nil {
		return nil, err
	}
	return s.users.UpdateUser(ctx, u)
}

func (s *userImpl) Delete(ctx context.Context, id, actorID int64) ([]int64, error) {
	return s.users.DeleteUser(ctx, id, actorID)
}

// AssertUnique checks user_name (always) and email/phone_number (when set) are
// not already taken, ignoring the record with excludeID (0 = none). A
// not-found from the repo is the expected "available" case. Checks run against
// active rows only, so soft-deleted values are reusable.
func AssertUnique(ctx context.Context, repo repository.UserRepo, u *entity.User, excludeID int64) error {
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
