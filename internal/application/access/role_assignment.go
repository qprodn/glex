package access

import (
	"context"
	"log/slog"

	authv1 "glex/api/iam/auth/v1"
	userv1 "glex/api/iam/user/v1"
	accessdomain "glex/internal/domain/access"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertRoleAssignment maps a accessdomain.RoleAssignment DO to the proto DTO.
func convertRoleAssignment(a *accessdomain.RoleAssignment) *authv1.RoleAssignment {
	return &authv1.RoleAssignment{
		Role:     convertRole(a.Role),
		Assigned: a.Assigned,
	}
}

// convertRoleUser maps a accessdomain.RoleUser DO to the proto User DTO —
// access's own projection of the by-role listing; the user module's converter
// is not shared across the module seam.
func convertRoleUser(m *accessdomain.RoleUser) *userv1.User {
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
func toUserRoles(assignments []*accessdomain.RoleAssignment) *authv1.UserRoles {
	items := make([]*authv1.RoleAssignment, 0, len(assignments))
	for _, a := range assignments {
		items = append(items, convertRoleAssignment(a))
	}
	return &authv1.UserRoles{Items: items}
}

// RoleAssignmentApplication implements the access span of the proto
// AuthService: the three user↔role assignment endpoints. The login/logout
// span of the same proto service lives with authn; the contracts split in
// ticket 06 moves these RPCs onto a standalone RoleAssignmentService.
type RoleAssignmentApplication struct {
	authv1.UnimplementedAuthServiceServer

	uc     *RoleAssignmentUsecase
	logger *slog.Logger
}

// NewRoleAssignmentApplication creates a RoleAssignmentApplication.
func NewRoleAssignmentApplication(uc *RoleAssignmentUsecase, logger *slog.Logger) *RoleAssignmentApplication {
	return &RoleAssignmentApplication{
		uc:     uc,
		logger: logger.With(slog.String("component", "application.access.role_assignment")),
	}
}

// ListUserRoles returns every active role with its assigned flag for the
// user (the assign dialog's data in one call).
func (s *RoleAssignmentApplication) ListUserRoles(ctx context.Context, req *authv1.ListUserRolesRequest) (*authv1.UserRoles, error) {
	assignments, err := s.uc.ListUserRoles(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	return toUserRoles(assignments), nil
}

// UpdateUserRoles replaces the user's Role Assignment (full replacement; an
// empty role_ids clears all) and returns the updated assignment.
func (s *RoleAssignmentApplication) UpdateUserRoles(ctx context.Context, req *authv1.UpdateUserRolesRequest) (*authv1.UserRoles, error) {
	assignments, err := s.uc.UpdateUserRoles(ctx, req.GetUserId(), req.GetRoleIds())
	if err != nil {
		return nil, err
	}
	return toUserRoles(assignments), nil
}

// ListRoleUsers lists the active users assigned the role, with AIP-160
// filter, AIP-132 order, and page-token pagination (same shape as
// ListUsers).
func (s *RoleAssignmentApplication) ListRoleUsers(ctx context.Context, req *authv1.ListRoleUsersRequest) (*userv1.UserSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
	)
	if err != nil {
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		// Default order: id as a stable tie-break for page-token pagination.
		orderBy.Fields = []ordering.Field{{Path: "user_id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	users, err := s.uc.ListRoleUsers(ctx, req.GetRoleId(),
		accessdomain.ListFilter(filter),
		accessdomain.ListOrderBy(orderBy),
		accessdomain.ListLimit(int(req.GetPageSize())),
		accessdomain.ListOffset(int(pageToken.Offset)),
	)
	if err != nil {
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
