package iam

import (
	"context"
	"log/slog"

	"glex/internal/crossdomain/authn"
	iamdomain "glex/internal/domain/iam"
)

// RoleAssignmentUsecase holds the user↔role assignment business rules.
type RoleAssignmentUsecase struct {
	userRepo iamdomain.UserRepo
	roleRepo iamdomain.RoleRepo
	linkRepo iamdomain.UserRoleRepo
	revoker  authn.SessionRevoker
	tx       Transaction
	logger   *slog.Logger
}

// NewRoleAssignmentUsecase creates a RoleAssignmentUsecase. userRepo/roleRepo
// back existence checks; linkRepo owns the sys_user_role rows; revoker
// performs Role Assignment revocations through the authn crossdomain
// contract; tx owns the Role Assignment replacement transaction boundary.
func NewRoleAssignmentUsecase(userRepo iamdomain.UserRepo, roleRepo iamdomain.RoleRepo, linkRepo iamdomain.UserRoleRepo, revoker authn.SessionRevoker, tx Transaction, logger *slog.Logger) *RoleAssignmentUsecase {
	return &RoleAssignmentUsecase{
		userRepo: userRepo,
		roleRepo: roleRepo,
		linkRepo: linkRepo,
		revoker:  revoker,
		tx:       tx,
		logger:   logger.With(slog.String("component", "application.iam.role_assignment")),
	}
}

// ListUserRoles returns every active role with its assigned flag for the
// user — the assign dialog's data in one call. The user must exist (and not
// be soft-deleted).
func (uc *RoleAssignmentUsecase) ListUserRoles(ctx context.Context, userID int64) ([]*iamdomain.RoleAssignment, error) {
	if _, err := uc.userRepo.FindByID(ctx, userID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleAssignments(ctx, userID)
}

// UpdateUserRoles replaces the user's Role Assignment: role_ids are validated
// (exist, not soft-deleted) and deduped, then stored as a full replacement
// (an empty list clears all) in one transaction that also revokes the user's
// Login Sessions. Redis revocation failure rolling back the PostgreSQL
// replacement is part of this Usecase's business contract. Returns the
// updated assignment.
func (uc *RoleAssignmentUsecase) UpdateUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]*iamdomain.RoleAssignment, error) {
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
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.linkRepo.ReplaceUserRoles(ctx, userID, unique)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.revoker, affected...)
	}); err != nil {
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
func (uc *RoleAssignmentUsecase) ListRoleUsers(ctx context.Context, roleID int64, opts ...iamdomain.ListOption) ([]*iamdomain.User, error) {
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
func (uc *RoleAssignmentUsecase) assertRolesAssignable(ctx context.Context, assignments []*iamdomain.RoleAssignment, roleIDs []int64) ([]int64, error) {
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
			return nil, iamdomain.ErrRoleNotFound
		}
	}
	return unique, nil
}
