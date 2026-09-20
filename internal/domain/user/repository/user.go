// Package repository holds the System User module's ports: the repository
// interface, its request/response shapes, and the ListOption family.
package repository

import (
	"context"

	"glex/internal/domain/user/entity"
	"glex/pkg/listq"
)

// ListUsersRequest is one System User list read: the shared query shape
// plus the department-subtree filter the application resolves through the
// org contract (ADR 0024) riding the domain List down as DeptIDs — empty
// filters nothing. Like the Query it carries, the request flows
// application → domain → repo unchanged.
type ListUsersRequest struct {
	listq.Query
	DeptIDs []int64
}

// DeleteUserRequest is one security delete: the System User to withdraw and
// the actor stamped into every audit field the write touches.
type DeleteUserRequest struct {
	ID      int64
	ActorID int64
}

// UserRepo is the repository interface for system users (the inversion seam
// declared in domain, implemented in data).
type UserRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.User, error)
	FindByUserName(ctx context.Context, userName string) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	FindByPhoneNumber(ctx context.Context, phone string) (*entity.User, error)
	ListUsers(ctx context.Context, req ListUsersRequest) ([]*entity.User, error)
	CreateUser(ctx context.Context, u *entity.User) (*entity.User, error)
	// UpdateUser returns the persisted record with its pre-image. The
	// Application service owns its transaction and session revocation.
	UpdateUser(ctx context.Context, u *entity.User) (*entity.UserUpdate, error)
	// DeleteUser is a security write: it soft-deletes the user and its Role
	// Assignment (ADR 0002 — no row is hard-deleted), stamping req.ActorID
	// into the audit fields, and returns the System User identities whose
	// sessions must be revoked (always {req.ID}).
	DeleteUser(ctx context.Context, req DeleteUserRequest) ([]int64, error)
	// CountByDeptID counts active users whose department reference points at
	// deptID — the read behind the org module's "a department with users
	// cannot be deleted or disabled" guards (ADR 0013).
	CountByDeptID(ctx context.Context, deptID int64) (int, error)
}
