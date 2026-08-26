package user

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

// User is a system-user domain object (系统用户), ported from RuoYi sys_user.
// Pure domain model: no proto, no storage tags.
type User struct {
	ID          int64
	UserName    string
	NickName    string
	Email       string
	PhoneNumber string
	Gender      int32
	// Password carries the caller's requested plaintext credential on
	// create/update requests only. It is never populated on reads and never
	// persisted by this module: the stored hash belongs to authn and is
	// written solely through its credential capability (ADR 0016).
	Password  string
	Status    int32
	Avatar    string
	Remark    string
	CreateBy  int64
	UpdateBy  int64
	LoginIP   string
	LoginDate *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionRevocationFrom classifies System User writes. The receiver is the
// requested change. Status changes always revoke all Sessions. Password
// requests (a non-empty request Password, delegated to authn's credential
// capability) use the caller's explicit self/admin policy; other profile
// writes do not affect authorization.
func (u *User) SessionRevocationFrom(current *User, passwordPolicy SessionRevocation) SessionRevocation {
	if u == nil || current == nil {
		return SessionRevocation{}
	}
	if u.Status != current.Status {
		return RevokeAllLoginSessions()
	}
	if u.Password != "" {
		return passwordPolicy
	}
	return SessionRevocation{}
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

// UserUpdate pairs the persisted System User with its pre-image so the
// Application Usecase can classify session revocation.
type UserUpdate struct {
	User     *User
	Previous *User
}

// UserRepo is the repository interface for system users (the inversion seam
// declared in domain, implemented in data).
type UserRepo interface {
	FindByID(ctx context.Context, id int64) (*User, error)
	FindByUserName(ctx context.Context, userName string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByPhoneNumber(ctx context.Context, phone string) (*User, error)
	ListUsers(ctx context.Context, opts ...ListOption) ([]*User, error)
	CreateUser(ctx context.Context, u *User) (*User, error)
	// UpdateUser returns the persisted record with its pre-image. The
	// Application Usecase owns its transaction and session revocation.
	UpdateUser(ctx context.Context, u *User) (*UserUpdate, error)
	// DeleteUser is a security write: it soft-deletes the user and
	// hard-deletes its Role Assignment, and returns the System User identities
	// whose sessions must be revoked (always {id}).
	DeleteUser(ctx context.Context, id int64) ([]int64, error)
}

var (
	// ErrUserNotFound: the requested user does not exist.
	ErrUserNotFound = errors.NotFound("USER_NOT_FOUND", "user not found")
	// ErrUserNameAmbiguous means an accepted best-effort uniqueness race left
	// more than one active System User with the same login identifier.
	ErrUserNameAmbiguous = errors.Conflict("USER_NAME_AMBIGUOUS", "user_name identifies multiple users")
	// ErrUserNameConflict: user_name already taken.
	ErrUserNameConflict = errors.Conflict("USER_NAME_CONFLICT", "user_name already exists")
	// ErrEmailConflict: email already taken.
	ErrEmailConflict = errors.Conflict("EMAIL_CONFLICT", "email already exists")
	// ErrPhoneConflict: phone_number already taken.
	ErrPhoneConflict = errors.Conflict("PHONE_CONFLICT", "phone_number already exists")
	// ErrPasswordRequired: create called without a password.
	ErrPasswordRequired = errors.BadRequest("PASSWORD_REQUIRED", "password is required")
)
