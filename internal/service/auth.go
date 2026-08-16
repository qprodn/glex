package service

import (
	"context"
	"strings"
	"time"

	authv1 "glex/api/system/auth/v1"
	userv1 "glex/api/system/user/v1"
	"glex/internal/biz"

	"github.com/go-kratos/kratos/v3/transport"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
)

// convertRoleAssignment maps a biz.RoleAssignment DO to the proto DTO.
func convertRoleAssignment(a *biz.RoleAssignment) *authv1.RoleAssignment {
	return &authv1.RoleAssignment{
		Role:     convertRole(a.Role),
		Assigned: a.Assigned,
	}
}

// toUserRoles maps a list of assignments to the UserRoles DTO.
func toUserRoles(assignments []*biz.RoleAssignment) *authv1.UserRoles {
	items := make([]*authv1.RoleAssignment, 0, len(assignments))
	for _, a := range assignments {
		items = append(items, convertRoleAssignment(a))
	}
	return &authv1.UserRoles{Items: items}
}

// AuthService implements the user↔role assignment service (auth module):
// assigning roles to a user and listing a role's users. Pure pass-through —
// business rules live in biz.AuthUsecase / the role usecase's guards.
type AuthService struct {
	authv1.UnimplementedAuthServiceServer

	uc *biz.AuthUsecase
}

// NewAuthService creates an AuthService.
func NewAuthService(uc *biz.AuthUsecase) *AuthService {
	return &AuthService{uc: uc}
}

// Login creates one Login Session and returns its bearer credential exactly
// once. Request metadata is read at this transport boundary and passed to biz.
func (s *AuthService) Login(ctx context.Context, req *authv1.LoginRequest) (*authv1.LoginReply, error) {
	sourceIP, userAgent := "", ""
	if transporter, ok := transport.FromServerContext(ctx); ok && transporter.RequestHeader() != nil {
		sourceIP = strings.TrimSpace(transporter.RequestHeader().Get("X-Forwarded-For"))
		userAgent = transporter.RequestHeader().Get("User-Agent")
	}
	result, err := s.uc.Login(ctx, &biz.LoginRequest{
		UserName:  req.GetUsername(),
		Password:  req.GetPassword(),
		SourceIP:  sourceIP,
		UserAgent: userAgent,
	})
	if err != nil {
		return nil, err
	}
	return &authv1.LoginReply{
		AccessToken: result.AccessToken,
		ExpireIn:    int64(result.ExpiresIn / time.Second),
	}, nil
}

// Logout succeeds anonymously and otherwise revokes only the current Login
// Session attached by OPTIONAL authentication.
func (s *AuthService) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	principal, _ := biz.PrincipalFromContext(ctx)
	if err := s.uc.Logout(ctx, principal); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListUserRoles returns every active role with its assigned flag for the
// user (the assign dialog's data in one call).
func (s *AuthService) ListUserRoles(ctx context.Context, req *authv1.ListUserRolesRequest) (*authv1.UserRoles, error) {
	assignments, err := s.uc.ListUserRoles(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	return toUserRoles(assignments), nil
}

// UpdateUserRoles replaces the user's Role Assignment (full replacement; an
// empty role_ids clears all) and returns the updated assignment.
func (s *AuthService) UpdateUserRoles(ctx context.Context, req *authv1.UpdateUserRolesRequest) (*authv1.UserRoles, error) {
	assignments, err := s.uc.UpdateUserRoles(ctx, req.GetUserId(), req.GetRoleIds())
	if err != nil {
		return nil, err
	}
	return toUserRoles(assignments), nil
}

// ListRoleUsers lists the active users assigned the role, with AIP-160
// filter, AIP-132 order, and page-token pagination (same shape as
// ListUsers).
func (s *AuthService) ListRoleUsers(ctx context.Context, req *authv1.ListRoleUsersRequest) (*userv1.UserSet, error) {
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
		biz.ListFilter(filter),
		biz.ListOrderBy(orderBy),
		biz.ListLimit(int(req.GetPageSize())),
		biz.ListOffset(int(pageToken.Offset)),
	)
	if err != nil {
		return nil, err
	}
	set := &userv1.UserSet{Users: make([]*userv1.User, 0, len(users))}
	for _, u := range users {
		set.Users = append(set.Users, convertUser(u))
	}
	if len(users) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}
