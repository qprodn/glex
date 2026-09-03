package access

import (
	"context"
	"log/slog"

	accessv1 "glex/api/access/v1"
	userv1 "glex/api/user/v1"
	"glex/internal/bizpkg"
	"glex/internal/crossdomain/authn"
	accessentity "glex/internal/domain/access/entity"
	accessservice "glex/internal/domain/access/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertRoleAssignment maps a accessentity.RoleAssignment DO to the proto DTO.
func convertRoleAssignment(a *accessentity.RoleAssignment) *accessv1.RoleAssignment {
	return &accessv1.RoleAssignment{
		Role:     convertRole(a.Role),
		Assigned: a.Assigned,
	}
}

// convertRoleUser maps a accessentity.RoleUser DO to the proto User DTO —
// access's own projection of the by-role listing; the user module's converter
// is not shared across the module seam.
func convertRoleUser(m *accessentity.RoleUser) *userv1.User {
	u := &userv1.User{
		UserId:      m.ID,
		UserName:    m.UserName,
		NickName:    m.NickName,
		Email:       m.Email,
		PhoneNumber: m.PhoneNumber,
		Gender:      m.Gender,
		Status:      m.Status,
		Avatar:      m.Avatar,
		Remark:      m.Remark,
		CreateBy:    m.CreateBy,
		UpdateBy:    m.UpdateBy,
		LoginIp:     m.LoginIP,
		CreatedAt:   timestamppb.New(m.CreatedAt),
		UpdatedAt:   timestamppb.New(m.UpdatedAt),
	}
	if m.LoginDate != nil {
		u.LoginDate = timestamppb.New(*m.LoginDate)
	}
	return u
}

// toUserRoles maps a list of assignments to the UserRoles DTO.
func toUserRoles(assignments []*accessentity.RoleAssignment) *accessv1.UserRoles {
	items := make([]*accessv1.RoleAssignment, 0, len(assignments))
	for _, a := range assignments {
		items = append(items, convertRoleAssignment(a))
	}
	return &accessv1.UserRoles{Items: items}
}

// RoleAssignmentApplication implements the access-owned
// RoleAssignmentService: the three user↔role assignment endpoints behind
// RuoYi's authRole dialog. It owns the assignment replacement's transaction
// boundary and the revocation through the authn crossdomain contract; all
// repository access and the intra-domain checks (user existence, role
// assignability) live in the domain service (ADR 0005).
type RoleAssignmentApplication struct {
	accessv1.UnimplementedRoleAssignmentServiceServer

	svc     accessservice.RoleAssignment
	revoker authn.SessionRevoker
	tx      Transaction
	logger  *slog.Logger
}

// NewRoleAssignmentApplication creates a RoleAssignmentApplication. svc is
// the Role Assignment domain service — the module's only repository holder;
// revoker performs Role Assignment revocations through the authn crossdomain
// contract; tx owns the Role Assignment replacement transaction boundary.
func NewRoleAssignmentApplication(svc accessservice.RoleAssignment, revoker authn.SessionRevoker, tx Transaction, logger *slog.Logger) *RoleAssignmentApplication {
	return &RoleAssignmentApplication{
		svc:     svc,
		revoker: revoker,
		tx:      tx,
		logger:  logger.With(slog.String("component", "application.access.role_assignment")),
	}
}

// ListUserRoles returns every active role with its assigned flag for the
// user (the assign dialog's data in one call). The user must exist (and not
// be soft-deleted).
func (s *RoleAssignmentApplication) ListUserRoles(ctx context.Context, req *accessv1.ListUserRolesRequest) (*accessv1.UserRoles, error) {
	assignments, err := s.svc.ListForUser(ctx, req.GetUserId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_user_roles", err)
		return nil, err
	}
	return toUserRoles(assignments), nil
}

// UpdateUserRoles replaces the user's Role Assignment (full replacement; an
// empty role_ids clears all) and returns the updated assignment. role_ids are
// validated (exist, not soft-deleted) and deduped, then stored in one
// transaction that also revokes the user's Login Sessions. Redis revocation
// failure rolling back the PostgreSQL replacement is part of this operation's
// business contract.
func (s *RoleAssignmentApplication) UpdateUserRoles(ctx context.Context, req *accessv1.UpdateUserRolesRequest) (*accessv1.UserRoles, error) {
	var assignments []*accessentity.RoleAssignment
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := s.svc.ReplaceForUser(ctx, req.GetUserId(), req.GetRoleIds(), bizpkg.AuditActor(ctx))
		if err != nil {
			return err
		}
		assignments = result.Assignments
		return revokeAllSystemUsers(ctx, s.revoker, result.AffectedUserIDs...)
	}); err != nil {
		logx.Propagate(ctx, s.logger, "access.update_user_roles", err)
		return nil, err
	}
	return toUserRoles(assignments), nil
}

// ListRoleUsers lists the active users assigned the role, with AIP-160
// filter, AIP-132 order, and page-token pagination (same shape as
// ListUsers).
func (s *RoleAssignmentApplication) ListRoleUsers(ctx context.Context, req *accessv1.ListRoleUsersRequest) (*userv1.UserSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_role_users", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_role_users", err)
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_role_users", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_role_users", err)
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		// Default order: id as a stable tie-break for page-token pagination.
		orderBy.Fields = []ordering.Field{{Path: "id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	users, err := s.svc.ListRoleUsers(ctx, req.GetRoleId(), filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_role_users", err)
		return nil, err
	}
	set := &userv1.UserSet{Users: make([]*userv1.User, 0, len(users))}
	for _, u := range users {
		set.Users = append(set.Users, convertRoleUser(u))
	}
	if len(users) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}
