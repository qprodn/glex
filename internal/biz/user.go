package biz

import (
	"context"
	"errors"
	"time"
)

// User is a system-user domain object (系统用户), ported from RuoYi sys_user.
// Pure biz model: no proto, no storage tags.
type User struct {
	ID          int64
	UserName    string
	NickName    string
	Email       string
	PhoneNumber string
	Gender      int32
	Password    string
	Status      int32
	Avatar      string
	Remark      string
	CreateBy    int64
	UpdateBy    int64
	LoginIP     string
	LoginDate   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UserRepo is the repository interface for system users (the inversion seam
// declared in biz, implemented in data).
type UserRepo interface {
	FindByID(ctx context.Context, id int64) (*User, error)
	FindByUserName(ctx context.Context, userName string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByPhoneNumber(ctx context.Context, phone string) (*User, error)
	ListUsers(ctx context.Context, opts ...ListOption) ([]*User, error)
	CreateUser(ctx context.Context, u *User) (*User, error)
	UpdateUser(ctx context.Context, u *User) (*User, error)
	DeleteUser(ctx context.Context, id int64) error
}

// UserUsecase holds the system-user business rules.
type UserUsecase struct {
	repo UserRepo
}

// NewUserUsecase creates a UserUsecase.
func NewUserUsecase(repo UserRepo) *UserUsecase {
	return &UserUsecase{repo: repo}
}

// CreateUser validates uniqueness, hashes the password, and persists a user.
// Uniqueness is defense-in-depth: the DB also enforces unique indexes and the
// repo maps a unique violation to the matching conflict error (covers races too).
func (uc *UserUsecase) CreateUser(ctx context.Context, u *User) (*User, error) {
	if u.Password == "" {
		return nil, ErrPasswordRequired
	}
	if err := uc.assertUnique(ctx, u); err != nil {
		return nil, err
	}
	hashed, err := hashPassword(u.Password)
	if err != nil {
		return nil, err
	}
	u.Password = hashed
	return uc.repo.CreateUser(ctx, u)
}

// UpdateUser applies a partial update. Empty password means "leave unchanged";
// a non-empty password is bcrypt-hashed before persisting. Uniqueness on update
// is enforced by the DB (the repo maps a violation to a conflict error).
func (uc *UserUsecase) UpdateUser(ctx context.Context, u *User) (*User, error) {
	if u.Password != "" {
		hashed, err := hashPassword(u.Password)
		if err != nil {
			return nil, err
		}
		u.Password = hashed
	}
	return uc.repo.UpdateUser(ctx, u)
}

// GetUser returns the user with the given id.
func (uc *UserUsecase) GetUser(ctx context.Context, id int64) (*User, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListUsers lists users matching the given options.
func (uc *UserUsecase) ListUsers(ctx context.Context, opts ...ListOption) ([]*User, error) {
	return uc.repo.ListUsers(ctx, opts...)
}

// DeleteUser soft-deletes the user with the given id.
func (uc *UserUsecase) DeleteUser(ctx context.Context, id int64) error {
	return uc.repo.DeleteUser(ctx, id)
}

// assertUnique checks user_name (always) and email/phone_number (when set) are
// not already taken. A not-found from the repo is the expected "available" case.
func (uc *UserUsecase) assertUnique(ctx context.Context, u *User) error {
	if existing, err := uc.repo.FindByUserName(ctx, u.UserName); err != nil {
		if !errors.Is(err, ErrUserNotFound) {
			return err
		}
	} else if existing != nil {
		return ErrUserNameConflict
	}
	if u.Email != "" {
		if existing, err := uc.repo.FindByEmail(ctx, u.Email); err != nil {
			if !errors.Is(err, ErrUserNotFound) {
				return err
			}
		} else if existing != nil {
			return ErrEmailConflict
		}
	}
	if u.PhoneNumber != "" {
		if existing, err := uc.repo.FindByPhoneNumber(ctx, u.PhoneNumber); err != nil {
			if !errors.Is(err, ErrUserNotFound) {
				return err
			}
		} else if existing != nil {
			return ErrPhoneConflict
		}
	}
	return nil
}
