package service

import (
	"context"
	"log/slog"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// RoleAssignment is the access domain service behind the Role Assignment
// (角色分配) endpoints: the only path to the user↔role link reads and writes
// (ADR 0005). Methods join the caller's ambient transaction; the Application
// service owns the transaction boundary and the cross-domain revocation.
// Every error a method returns to its caller first logs its Warn breadcrumb
// (ADR 0011).
type RoleAssignment interface {
	// ListForUser returns every active role with whether the user is assigned
	// it, gated on the user existing and not being soft-deleted.
	ListForUser(ctx context.Context, userID int64) ([]*entity.RoleAssignment, error)
	// ReplaceForUser validates roleIDs (exist, not soft-deleted), replaces the
	// user's Role Assignment in full, and returns the freshly-listed
	// assignments with their Assigned flags reflecting the persisted set plus
	// the identities whose Authorization Snapshot the write invalidates.
	ReplaceForUser(ctx context.Context, userID int64, roleIDs []int64, actorID int64) (*RoleAssignmentReplacement, error)
	// ListRoleUsers lists the active users assigned the role, gated on the
	// role existing, matching the given filter/order/offset/limit.
	ListRoleUsers(ctx context.Context, roleID int64, q listq.Query) ([]*entity.RoleUser, error)
}

// RoleAssignmentReplacement is the ReplaceForUser result.
type RoleAssignmentReplacement struct {
	Assignments     []*entity.RoleAssignment
	AffectedUserIDs []int64
}

type roleAssignmentImpl struct {
	roles     repository.RoleRepo
	userRoles repository.UserRoleRepo
	logger    *slog.Logger
}

// NewRoleAssignmentService creates the Role Assignment domain service. roles
// backs the role existence check; userRoles owns the sys_user_role rows and
// the by-user existence gate.
func NewRoleAssignmentService(roles repository.RoleRepo, userRoles repository.UserRoleRepo, logger *slog.Logger) RoleAssignment {
	return &roleAssignmentImpl{
		roles:     roles,
		userRoles: userRoles,
		logger:    logger.With(slog.String("component", "domain.access.svc")),
	}
}

func (s *roleAssignmentImpl) ListForUser(ctx context.Context, userID int64) ([]*entity.RoleAssignment, error) {
	if err := AssertUserExists(ctx, s.userRoles, userID); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.list_assignments_for_user", err)
		return nil, err
	}
	assignments, err := s.userRoles.ListRoleAssignments(ctx, userID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.list_assignments_for_user", err)
		return nil, err
	}
	return assignments, nil
}

func (s *roleAssignmentImpl) ReplaceForUser(ctx context.Context, userID int64, roleIDs []int64, actorID int64) (*RoleAssignmentReplacement, error) {
	if err := AssertUserExists(ctx, s.userRoles, userID); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_assignments", err)
		return nil, err
	}
	assignments, err := s.userRoles.ListRoleAssignments(ctx, userID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_assignments", err)
		return nil, err
	}
	unique, err := AssertRolesAssignable(assignments, roleIDs)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_assignments", err)
		return nil, err
	}
	affected, err := s.userRoles.ReplaceUserRoles(ctx, userID, unique, actorID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_assignments", err)
		return nil, err
	}
	assigned := make(map[int64]struct{}, len(unique))
	for _, id := range unique {
		assigned[id] = struct{}{}
	}
	for _, a := range assignments {
		_, a.Assigned = assigned[a.Role.ID]
	}
	return &RoleAssignmentReplacement{Assignments: assignments, AffectedUserIDs: affected}, nil
}

func (s *roleAssignmentImpl) ListRoleUsers(ctx context.Context, roleID int64, q listq.Query) ([]*entity.RoleUser, error) {
	if _, err := s.roles.FindByID(ctx, roleID); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.list_role_users", err)
		return nil, err
	}
	users, err := s.userRoles.ListRoleUsers(ctx, roleID, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.list_role_users", err)
		return nil, err
	}
	return users, nil
}

// AssertUserExists gates the by-user endpoints on the System User existing
// (and not being soft-deleted) — access's own copy of the reason, answered by
// the assignment storage's user edge. Its caller's method boundary owns the
// breadcrumb (ADR 0011).
func AssertUserExists(ctx context.Context, linkRepo repository.UserRoleRepo, userID int64) error {
	exists, err := linkRepo.UserExists(ctx, userID)
	if err != nil {
		return err
	}
	if !exists {
		return access.ErrUserNotFound
	}
	return nil
}

// AssertRolesAssignable dedupes roleIDs and checks every id exists and is not
// soft-deleted. The active-role set is the freshly-read assignment list — the
// same data the response carries, so no second query (Menu Grant instead
// re-reads via ListByIDs; here the read is needed anyway for the reply). An
// empty list is legal — it means "no roles" — and passes without touching
// anything.
func AssertRolesAssignable(assignments []*entity.RoleAssignment, roleIDs []int64) ([]int64, error) {
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
			return nil, access.ErrRoleNotFound
		}
	}
	return unique, nil
}
