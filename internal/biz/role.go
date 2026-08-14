package biz

import (
	"context"
	"errors"
	"time"
)

// Role is a role domain object (角色), ported from RuoYi sys_role.
// Pure biz model: no proto, no storage tags. Basic CRUD only — no
// associations with other tables (role↔menu, user↔role are deferred).
type Role struct {
	ID        int64
	RoleName  string
	RoleKey   string
	RoleSort  int
	Status    int32
	Remark    string
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RoleRepo is the repository interface for roles (the inversion seam declared
// in biz, implemented in data).
type RoleRepo interface {
	FindByID(ctx context.Context, id int64) (*Role, error)
	FindByRoleName(ctx context.Context, roleName string) (*Role, error)
	FindByRoleKey(ctx context.Context, roleKey string) (*Role, error)
	ListRoles(ctx context.Context, opts ...ListOption) ([]*Role, error)
	CreateRole(ctx context.Context, r *Role) (*Role, error)
	UpdateRole(ctx context.Context, r *Role) (*Role, error)
	DeleteRole(ctx context.Context, id int64) error
}

// RoleUsecase holds the role business rules.
type RoleUsecase struct {
	repo RoleRepo
}

// NewRoleUsecase creates a RoleUsecase.
func NewRoleUsecase(repo RoleRepo) *RoleUsecase {
	return &RoleUsecase{repo: repo}
}

// CreateRole validates and persists a role: role_name/role_key must be
// non-blank and unique among active roles. Uniqueness is a code check
// (repo-wide convention; the DB has no unique constraints) — racy under
// concurrent creates, accepted.
func (uc *RoleUsecase) CreateRole(ctx context.Context, r *Role) (*Role, error) {
	if err := uc.validate(r); err != nil {
		return nil, err
	}
	if err := uc.assertUnique(ctx, r, 0); err != nil {
		return nil, err
	}
	return uc.repo.CreateRole(ctx, r)
}

// UpdateRole validates and persists a role, excluding the record being
// updated from the uniqueness checks.
func (uc *RoleUsecase) UpdateRole(ctx context.Context, r *Role) (*Role, error) {
	if err := uc.validate(r); err != nil {
		return nil, err
	}
	if err := uc.assertUnique(ctx, r, r.ID); err != nil {
		return nil, err
	}
	return uc.repo.UpdateRole(ctx, r)
}

// GetRole returns the role with the given id.
func (uc *RoleUsecase) GetRole(ctx context.Context, id int64) (*Role, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListRoles lists roles matching the given options.
func (uc *RoleUsecase) ListRoles(ctx context.Context, opts ...ListOption) ([]*Role, error) {
	return uc.repo.ListRoles(ctx, opts...)
}

// DeleteRole soft-deletes the role with the given id.
func (uc *RoleUsecase) DeleteRole(ctx context.Context, id int64) error {
	return uc.repo.DeleteRole(ctx, id)
}

// validate rejects blank role_name/role_key (Q4: non-blank is the only rule).
func (uc *RoleUsecase) validate(r *Role) error {
	if r.RoleName == "" || r.RoleKey == "" {
		return ErrRoleInvalid
	}
	return nil
}

// assertUnique checks role_name and role_key are not taken by another active
// role, ignoring the record with excludeID (0 = none). A not-found from the
// repo is the expected "available" case. Checks run against active rows only,
// so soft-deleted values are reusable.
func (uc *RoleUsecase) assertUnique(ctx context.Context, r *Role, excludeID int64) error {
	if existing, err := uc.repo.FindByRoleName(ctx, r.RoleName); err != nil {
		if !errors.Is(err, ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return ErrRoleNameConflict
	}
	if existing, err := uc.repo.FindByRoleKey(ctx, r.RoleKey); err != nil {
		if !errors.Is(err, ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return ErrRoleKeyConflict
	}
	return nil
}
