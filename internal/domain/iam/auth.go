package iam

import (
	"context"
)

// RoleAssignment is one role with whether the user is assigned it — the auth
// module's read shape for a user's Role Assignment (角色分配), mirroring
// RuoYi's authRole dialog data (all roles + checked flag).
type RoleAssignment struct {
	Role     *Role
	Assigned bool
}

// UserRoleRepo is the repository interface for user↔role links (the inversion
// seam declared in domain, implemented in data). The link table is sys_user_role
// (RuoYi shape: composite PK (user_id, role_id), no timestamps, no soft
// delete — rows are hard-deleted on full replacement and user delete).
type UserRoleRepo interface {
	// ListRoleAssignments returns every active role with whether the user is
	// assigned it, ordered by (role_sort, role_id).
	ListRoleAssignments(ctx context.Context, userID int64) ([]*RoleAssignment, error)
	// ReplaceUserRoles is a security write: it atomically replaces the user's
	// Role Assignment with roleIDs (full replacement; an empty list clears
	// all). roleIDs are stored verbatim after AuthUsecase validation and
	// deduplication; affected System User identities ({userID}) are returned.
	ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]int64, error)
	// ListRoleUsers lists the active users assigned the role, matching the
	// given options (filter/order/offset/limit).
	ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*User, error)
	// CountUserRoles counts the active users assigned the role — the guard
	// behind "a role with users cannot be deleted or disabled" (RuoYi:
	// 已分配，不能删除/禁用).
	CountUserRoles(ctx context.Context, roleID int64) (int, error)
}
