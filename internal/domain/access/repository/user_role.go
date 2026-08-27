package repository

import (
	"context"
	"time"

	"glex/internal/domain/access/entity"
)

// RoleAssignment is one role with whether the user is assigned it — the
// role-assignment read shape for a user's Role Assignment (角色分配), mirroring
// RuoYi's authRole dialog data (all roles + checked flag).
type RoleAssignment struct {
	Role     *entity.Role
	Assigned bool
}

// RoleUser is the System User projection the by-role endpoint returns: a flat
// profile snapshot owned by access because the endpoint and its link storage
// are access's (ADR 0016). It is a read shape, not the user module's
// aggregate.
type RoleUser struct {
	ID          int64
	UserName    string
	NickName    string
	Email       string
	PhoneNumber string
	Gender      int32
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

// UserRoleRepo is the repository interface for user↔role links (the inversion
// seam declared in domain, implemented in data). The link table is sys_user_role
// (RuoYi shape: composite PK (user_id, role_id), no timestamps, no soft
// delete — rows are hard-deleted on full replacement and user delete).
type UserRoleRepo interface {
	// UserExists reports whether the System User exists and is not
	// soft-deleted — the existence gate behind the by-user endpoints.
	UserExists(ctx context.Context, userID int64) (bool, error)
	// ListRoleAssignments returns every active role with whether the user is
	// assigned it, ordered by (role_sort, role_id).
	ListRoleAssignments(ctx context.Context, userID int64) ([]*RoleAssignment, error)
	// ReplaceUserRoles is a security write: it atomically replaces the user's
	// Role Assignment with roleIDs (full replacement; an empty list clears
	// all). roleIDs are stored verbatim after caller validation and
	// deduplication; affected System User identities ({userID}) are returned.
	ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]int64, error)
	// ListRoleUsers lists the active users assigned the role, matching the
	// given options (filter/order/offset/limit).
	ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*RoleUser, error)
	// CountUserRoles counts the active users assigned the role — the guard
	// behind "a role with users cannot be deleted or disabled" (RuoYi:
	// 已分配，不能删除/禁用).
	CountUserRoles(ctx context.Context, roleID int64) (int, error)
}
