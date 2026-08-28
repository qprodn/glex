package repository

import (
	"context"

	"glex/internal/domain/access/entity"
)

// UserRoleRepo is the repository interface for user↔role links (the inversion
// seam declared in domain, implemented in data). The link table is
// sys_user_role (snowflake PK id, audit columns, soft delete —
// ADR 0002; one active row per (user_id, role_id) pair).
type UserRoleRepo interface {
	// UserExists reports whether the System User exists and is not
	// soft-deleted — the existence gate behind the by-user endpoints.
	UserExists(ctx context.Context, userID int64) (bool, error)
	// ListRoleAssignments returns every active role with whether the user is
	// assigned it, ordered by (role_sort, id).
	ListRoleAssignments(ctx context.Context, userID int64) ([]*entity.RoleAssignment, error)
	// ReplaceUserRoles is a security write: it atomically replaces the user's
	// Role Assignment with roleIDs (full replacement of the active set; an
	// empty list clears all), stamping actorID into the audit fields of both
	// the withdrawn and the inserted rows. roleIDs are stored verbatim after
	// caller validation and deduplication; affected System User identities
	// ({userID}) are returned.
	ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64, actorID int64) ([]int64, error)
	// ListRoleUsers lists the active users assigned the role, matching the
	// given options (filter/order/offset/limit).
	ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*entity.RoleUser, error)
	// CountUserRoles counts the active users assigned the role — the guard
	// behind "a role with users cannot be deleted or disabled" (RuoYi:
	// 已分配，不能删除/禁用).
	CountUserRoles(ctx context.Context, roleID int64) (int, error)
}
