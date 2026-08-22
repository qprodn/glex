package iam

import (
	"context"
	"time"
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

// UserUpdate pairs the persisted System User with its pre-image, both read
// under the System User authorization lock. The usecase classifies the
// revocation policy from the pair and performs the revocation itself as the
// last step of its InTx closure.
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
	// UpdateUser is a security write: it takes the System User authorization
	// lock, mutates under it, and returns the persisted record with its
	// pre-image. Pair the result's revocation policy with SessionRevocation
	// inside Transaction.InTx (see UserUsecase.UpdateUser) — calling it
	// directly performs no revocation.
	UpdateUser(ctx context.Context, u *User) (*UserUpdate, error)
	// DeleteUser is a security write: it soft-deletes the user and
	// hard-deletes its Role Assignment under the authorization locks, and
	// returns the System User identities whose sessions must be revoked
	// (always {id}).
	DeleteUser(ctx context.Context, id int64) ([]int64, error)
}
