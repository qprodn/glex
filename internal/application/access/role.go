package access

import (
	"context"
	"log/slog"

	rolev1 "glex/api/iam/role/v1"
	accessdomain "glex/internal/domain/access"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertRole maps a accessdomain.Role DO to the proto Role DTO.
func convertRole(r *accessdomain.Role) *rolev1.Role {
	return &rolev1.Role{
		RoleId:            r.ID,
		RoleName:          r.RoleName,
		RoleKey:           r.RoleKey,
		RoleSort:          int32(r.RoleSort),
		Status:            r.Status,
		MenuCheckStrictly: r.MenuCheckStrictly,
		Remark:            r.Remark,
		CreateBy:          r.CreateBy,
		UpdateBy:          r.UpdateBy,
		CreatedAt:         timestamppb.New(r.CreatedAt),
		UpdatedAt:         timestamppb.New(r.UpdatedAt),
	}
}

// RoleApplication implements the system-role CRUD service.
//
// Authentication and the operation's Functional Permission policy are
// enforced by the registered HTTP middleware; this adapter only translates
// DTOs and delegates business rules.
type RoleApplication struct {
	rolev1.UnimplementedRoleServiceServer

	uc     *RoleUsecase
	logger *slog.Logger
}

// NewRoleApplication creates a RoleApplication.
func NewRoleApplication(uc *RoleUsecase, logger *slog.Logger) *RoleApplication {
	return &RoleApplication{
		uc:     uc,
		logger: logger.With(slog.String("component", "application.role")),
	}
}

// CreateRole creates a role with its optional initial Menu Grant. The
// presence-aware menu_check_strictly defaults to true (RuoYi default) when
// absent.
func (s *RoleApplication) CreateRole(ctx context.Context, req *rolev1.CreateRoleRequest) (*rolev1.Role, error) {
	in := req.GetRole()
	strict := true
	if req.MenuCheckStrictly != nil {
		strict = *req.MenuCheckStrictly
	}
	created, err := s.uc.CreateRole(ctx, &accessdomain.Role{
		RoleName:          in.GetRoleName(),
		RoleKey:           in.GetRoleKey(),
		RoleSort:          int(in.GetRoleSort()),
		Status:            in.GetStatus(),
		MenuCheckStrictly: strict,
		Remark:            in.GetRemark(),
	}, req.GetMenuIds())
	if err != nil {
		return nil, err
	}
	return convertRole(created), nil
}

// GetRole retrieves a role by id.
func (s *RoleApplication) GetRole(ctx context.Context, req *rolev1.GetRoleRequest) (*rolev1.Role, error) {
	r, err := s.uc.GetRole(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	return convertRole(r), nil
}

// ListRoles lists roles with AIP-160 filter, AIP-132 order, and page-token
// pagination.
func (s *RoleApplication) ListRoles(ctx context.Context, req *rolev1.ListRolesRequest) (*rolev1.RoleSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("role_name", filtering.TypeString),
		filtering.DeclareIdent("role_key", filtering.TypeString),
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
		// Default order: display order, then id as a stable tie-break for
		// page-token pagination.
		orderBy.Fields = []ordering.Field{{Path: "role_sort"}, {Path: "role_id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	roles, err := s.uc.ListRoles(ctx,
		accessdomain.ListFilter(filter),
		accessdomain.ListOrderBy(orderBy),
		accessdomain.ListLimit(int(req.GetPageSize())),
		accessdomain.ListOffset(int(pageToken.Offset)),
	)
	if err != nil {
		return nil, err
	}
	set := &rolev1.RoleSet{Roles: make([]*rolev1.Role, 0, len(roles))}
	for _, r := range roles {
		set.Roles = append(set.Roles, convertRole(r))
	}
	if len(roles) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// UpdateRole partially updates a role via field-mask. It loads the current
// state as a DTO, applies the mask onto it, then persists.
func (s *RoleApplication) UpdateRole(ctx context.Context, req *rolev1.UpdateRoleRequest) (*rolev1.Role, error) {
	current, err := s.GetRole(ctx, &rolev1.GetRoleRequest{RoleId: req.GetRole().GetRoleId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetRole())
	updated, err := s.uc.UpdateRole(ctx, &accessdomain.Role{
		ID:       current.GetRoleId(),
		RoleName: current.GetRoleName(),
		RoleKey:  current.GetRoleKey(),
		RoleSort: int(current.GetRoleSort()),
		Status:   current.GetStatus(),
		Remark:   current.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return convertRole(updated), nil
}

// DeleteRole soft-deletes a role by id (and its Menu Grant, repo side).
func (s *RoleApplication) DeleteRole(ctx context.Context, req *rolev1.DeleteRoleRequest) (*emptypb.Empty, error) {
	if err := s.uc.DeleteRole(ctx, req.GetRoleId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListRoleMenus returns the role's checkedKeys (granted menu ids shaped by
// menu_check_strictly).
func (s *RoleApplication) ListRoleMenus(ctx context.Context, req *rolev1.ListRoleMenusRequest) (*rolev1.RoleMenus, error) {
	ids, err := s.uc.ListRoleMenus(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	return &rolev1.RoleMenus{MenuIds: ids}, nil
}

// UpdateRoleMenus replaces the role's Menu Grant and menu_check_strictly in
// one transaction; the response echoes the persisted (deduped) grant.
func (s *RoleApplication) UpdateRoleMenus(ctx context.Context, req *rolev1.UpdateRoleMenusRequest) (*rolev1.RoleMenus, error) {
	strict := true
	if req.MenuCheckStrictly != nil {
		strict = *req.MenuCheckStrictly
	}
	ids, err := s.uc.UpdateRoleMenus(ctx, req.GetRoleId(), req.GetMenuIds(), strict)
	if err != nil {
		return nil, err
	}
	return &rolev1.RoleMenus{MenuIds: ids}, nil
}
