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

// CurrentUserInfo combines the current System User record with the immutable
// Role/Functional-Permission view authenticated into the request Principal.
// Password remains on User only for credential/update workflows and is never
// projected by the service's dedicated current-user DTO conversion.
type CurrentUserInfo struct {
	User        *User
	RoleKeys    []string
	Permissions []string
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
	UpdateUser(ctx context.Context, u *User, passwordPolicy SessionRevocation) (*User, error)
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
// Uniqueness is a code check against active rows (repo-wide convention; the DB
// has no unique constraints) — racy under concurrent creates, accepted.
func (uc *UserUsecase) CreateUser(ctx context.Context, u *User) (*User, error) {
	if u.Password == "" {
		return nil, ErrPasswordRequired
	}
	if err := uc.assertUnique(ctx, u, 0); err != nil {
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
// a non-empty password is bcrypt-hashed before persisting. Uniqueness is
// checked in code, excluding the record being updated.
func (uc *UserUsecase) UpdateUser(ctx context.Context, u *User) (*User, error) {
	if err := uc.assertUnique(ctx, u, u.ID); err != nil {
		return nil, err
	}
	if u.Password != "" {
		hashed, err := hashPassword(u.Password)
		if err != nil {
			return nil, err
		}
		u.Password = hashed
	}
	passwordPolicy := RevokeAllLoginSessions()
	if u.Password != "" {
		if principal, ok := PrincipalFromContext(ctx); ok && principal.SystemUserID() == u.ID {
			passwordPolicy = PreserveLoginSession(principal.SessionID())
		}
	}
	return uc.repo.UpdateUser(ctx, u, passwordPolicy)
}

// GetUser returns the user with the given id.
func (uc *UserUsecase) GetUser(ctx context.Context, id int64) (*User, error) {
	return uc.repo.FindByID(ctx, id)
}

// GetCurrentUserInfo reads the System User record afresh while retaining Roles
// and Functional Permissions from the authenticated Authorization Snapshot.
func (uc *UserUsecase) GetCurrentUserInfo(ctx context.Context, principal *Principal) (*CurrentUserInfo, error) {
	if principal == nil {
		return nil, ErrCredentialMissing
	}
	u, err := uc.repo.FindByID(ctx, principal.SystemUserID())
	if err != nil {
		return nil, err
	}
	return &CurrentUserInfo{
		User:        u,
		RoleKeys:    principal.RoleKeys(),
		Permissions: principal.Permissions(),
	}, nil
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
// not already taken, ignoring the record with excludeID (0 = none). A
// not-found from the repo is the expected "available" case. Checks run against
// active rows only, so soft-deleted values are reusable.
func (uc *UserUsecase) assertUnique(ctx context.Context, u *User, excludeID int64) error {
	if existing, err := uc.repo.FindByUserName(ctx, u.UserName); err != nil {
		if !errors.Is(err, ErrUserNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return ErrUserNameConflict
	}
	if u.Email != "" {
		if existing, err := uc.repo.FindByEmail(ctx, u.Email); err != nil {
			if !errors.Is(err, ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return ErrEmailConflict
		}
	}
	if u.PhoneNumber != "" {
		if existing, err := uc.repo.FindByPhoneNumber(ctx, u.PhoneNumber); err != nil {
			if !errors.Is(err, ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return ErrPhoneConflict
		}
	}
	return nil
}
