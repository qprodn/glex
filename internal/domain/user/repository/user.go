// Package repository holds the System User module's ports: the repository
// interface, its request/response shapes, and the ListOption family.
package repository

import (
	"context"

	"glex/internal/domain/user/entity"
)

// UserUpdate pairs the persisted System User with its pre-image so the
// Application service can classify session revocation.
type UserUpdate struct {
	User     *entity.User
	Previous *entity.User
}

// UserRepo is the repository interface for system users (the inversion seam
// declared in domain, implemented in data).
type UserRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.User, error)
	FindByUserName(ctx context.Context, userName string) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	FindByPhoneNumber(ctx context.Context, phone string) (*entity.User, error)
	ListUsers(ctx context.Context, opts ...ListOption) ([]*entity.User, error)
	CreateUser(ctx context.Context, u *entity.User) (*entity.User, error)
	// UpdateUser returns the persisted record with its pre-image. The
	// Application service owns its transaction and session revocation.
	UpdateUser(ctx context.Context, u *entity.User) (*UserUpdate, error)
	// DeleteUser is a security write: it soft-deletes the user and its Role
	// Assignment (ADR 0002 — no row is hard-deleted), stamping actorID into
	// the audit fields, and returns the System User identities whose sessions
	// must be revoked (always {id}).
	DeleteUser(ctx context.Context, id int64, actorID int64) ([]int64, error)
}
