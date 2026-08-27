package access

import (
	"context"
	"log/slog"

	"glex/internal/crossdomain/authn"
	accessrepo "glex/internal/domain/access/repository"
	accessservice "glex/internal/domain/access/service"
)

// RoleAssignmentUsecase holds the user↔role assignment business rules.
type RoleAssignmentUsecase struct {
	roleRepo accessrepo.RoleRepo
	linkRepo accessrepo.UserRoleRepo
	revoker  authn.SessionRevoker
	tx       Transaction
	logger   *slog.Logger
}

// NewRoleAssignmentUsecase creates a RoleAssignmentUsecase. roleRepo backs the
// role existence check; linkRepo owns the sys_user_role rows and the by-user
// existence gate; revoker performs Role Assignment revocations through the
// authn crossdomain contract; tx owns the Role Assignment replacement
// transaction boundary.
func NewRoleAssignmentUsecase(roleRepo accessrepo.RoleRepo, linkRepo accessrepo.UserRoleRepo, revoker authn.SessionRevoker, tx Transaction, logger *slog.Logger) *RoleAssignmentUsecase {
	return &RoleAssignmentUsecase{
		roleRepo: roleRepo,
		linkRepo: linkRepo,
		revoker:  revoker,
		tx:       tx,
		logger:   logger.With(slog.String("component", "application.access.role_assignment")),
	}
}

// ListUserRoles returns every active role with its assigned flag for the
// user — the assign dialog's data in one call. The user must exist (and not
// be soft-deleted).
func (uc *RoleAssignmentUsecase) ListUserRoles(ctx context.Context, userID int64) ([]*accessrepo.RoleAssignment, error) {
	if err := accessservice.AssertUserExists(ctx, uc.linkRepo, userID); err != nil {
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
func (uc *RoleAssignmentUsecase) UpdateUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]*accessrepo.RoleAssignment, error) {
	if err := accessservice.AssertUserExists(ctx, uc.linkRepo, userID); err != nil {
		return nil, err
	}
	assignments, err := uc.linkRepo.ListRoleAssignments(ctx, userID)
	if err != nil {
		return nil, err
	}
	unique, err := accessservice.AssertRolesAssignable(assignments, roleIDs)
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
func (uc *RoleAssignmentUsecase) ListRoleUsers(ctx context.Context, roleID int64, opts ...accessrepo.ListOption) ([]*accessrepo.RoleUser, error) {
	if _, err := uc.roleRepo.FindByID(ctx, roleID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleUsers(ctx, roleID, opts...)
}
