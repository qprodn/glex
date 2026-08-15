package biz

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
// seam declared in biz, implemented in data). The link table is sys_user_role
// (RuoYi shape: composite PK (user_id, role_id), no timestamps, no soft
// delete — rows are hard-deleted on full replacement and user delete).
type UserRoleRepo interface {
	// ListRoleAssignments returns every active role with whether the user is
	// assigned it, ordered by (role_sort, role_id).
	ListRoleAssignments(ctx context.Context, userID int64) ([]*RoleAssignment, error)
	// ReplaceUserRoles atomically replaces the user's Role Assignment with
	// roleIDs (full replacement; an empty list clears all). roleIDs are stored
	// verbatim — validated and deduped by the usecase first.
	ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) error
	// ListRoleUsers lists the active users assigned the role, matching the
	// given options (filter/order/offset/limit).
	ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*User, error)
	// CountUserRoles counts the active users assigned the role — the guard
	// behind "a role with users cannot be deleted or disabled" (RuoYi:
	// 已分配，不能删除/禁用).
	CountUserRoles(ctx context.Context, roleID int64) (int, error)
}

// AuthUsecase holds the user↔role assignment business rules (auth module).
type AuthUsecase struct {
	userRepo UserRepo
	roleRepo RoleRepo
	linkRepo UserRoleRepo
}

// NewAuthUsecase creates an AuthUsecase. userRepo/roleRepo back existence
// checks; linkRepo owns the sys_user_role rows.
func NewAuthUsecase(userRepo UserRepo, roleRepo RoleRepo, linkRepo UserRoleRepo) *AuthUsecase {
	return &AuthUsecase{userRepo: userRepo, roleRepo: roleRepo, linkRepo: linkRepo}
}

// ListUserRoles returns every active role with its assigned flag for the
// user — the assign dialog's data in one call. The user must exist (and not
// be soft-deleted).
func (uc *AuthUsecase) ListUserRoles(ctx context.Context, userID int64) ([]*RoleAssignment, error) {
	if _, err := uc.userRepo.FindByID(ctx, userID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleAssignments(ctx, userID)
}

// UpdateUserRoles replaces the user's Role Assignment: role_ids are validated
// (exist, not soft-deleted) and deduped, then stored as a full replacement
// (an empty list clears all). Returns the updated assignment.
func (uc *AuthUsecase) UpdateUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]*RoleAssignment, error) {
	if _, err := uc.userRepo.FindByID(ctx, userID); err != nil {
		return nil, err
	}
	assignments, err := uc.linkRepo.ListRoleAssignments(ctx, userID)
	if err != nil {
		return nil, err
	}
	unique, err := uc.assertRolesAssignable(ctx, assignments, roleIDs)
	if err != nil {
		return nil, err
	}
	if err := uc.linkRepo.ReplaceUserRoles(ctx, userID, unique); err != nil {
		return nil, err
	}
	assigned := make(map[int64]struct{}, len(unique))
	for _, id := range unique {
		assigned[id] = struct{}{}
	}
	for _, a := range assignments {
		_, a.Assigned = assigned[a.Role.ID]
	}
	return assignments, nil
}

// ListRoleUsers lists the active users assigned the given role. The role must
// exist (and not be soft-deleted).
func (uc *AuthUsecase) ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*User, error) {
	if _, err := uc.roleRepo.FindByID(ctx, roleID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleUsers(ctx, roleID, opts...)
}

// assertRolesAssignable dedupes roleIDs and checks every id exists and is not
// soft-deleted. The active-role set is the freshly-read assignment list — the
// same data the response carries, so no second query (Menu Grant instead
// re-reads via ListByIDs; here the read is needed anyway for the reply). An
// empty list is legal — it means "no roles" — and passes without touching
// anything.
func (uc *AuthUsecase) assertRolesAssignable(ctx context.Context, assignments []*RoleAssignment, roleIDs []int64) ([]int64, error) {
	unique := dedupeIDs(roleIDs)
	if len(unique) == 0 {
		return unique, nil
	}
	byID := make(map[int64]struct{}, len(assignments))
	for _, a := range assignments {
		byID[a.Role.ID] = struct{}{}
	}
	for _, id := range unique {
		if _, ok := byID[id]; !ok {
			return nil, ErrRoleNotFound
		}
	}
	return unique, nil
}
