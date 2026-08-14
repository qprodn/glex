package service

import (
	"context"

	rolev1 "glex/api/system/role/v1"
	"glex/internal/biz"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertRole maps a biz.Role DO to the proto Role DTO.
func convertRole(r *biz.Role) *rolev1.Role {
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

// RoleService implements the system-role CRUD service.
//
// No authz here by design: the global auth middleware will be added by the
// auth module, and finer permission checks belong to the RBAC/auth module.
type RoleService struct {
	rolev1.UnimplementedRoleServiceServer

	uc *biz.RoleUsecase
}

// NewRoleService creates a RoleService.
func NewRoleService(uc *biz.RoleUsecase) *RoleService {
	return &RoleService{uc: uc}
}

// CreateRole creates a role with its optional initial Menu Grant. The
// presence-aware menu_check_strictly defaults to true (RuoYi default) when
// absent.
func (s *RoleService) CreateRole(ctx context.Context, req *rolev1.CreateRoleRequest) (*rolev1.Role, error) {
	in := req.GetRole()
	strict := true
	if req.MenuCheckStrictly != nil {
		strict = *req.MenuCheckStrictly
	}
	created, err := s.uc.CreateRole(ctx, &biz.Role{
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
func (s *RoleService) GetRole(ctx context.Context, req *rolev1.GetRoleRequest) (*rolev1.Role, error) {
	r, err := s.uc.GetRole(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	return convertRole(r), nil
}

// ListRoles lists roles with AIP-160 filter, AIP-132 order, and page-token
// pagination.
func (s *RoleService) ListRoles(ctx context.Context, req *rolev1.ListRolesRequest) (*rolev1.RoleSet, error) {
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
		biz.ListFilter(filter),
		biz.ListOrderBy(orderBy),
		biz.ListLimit(int(req.GetPageSize())),
		biz.ListOffset(int(pageToken.Offset)),
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
func (s *RoleService) UpdateRole(ctx context.Context, req *rolev1.UpdateRoleRequest) (*rolev1.Role, error) {
	current, err := s.GetRole(ctx, &rolev1.GetRoleRequest{RoleId: req.GetRole().GetRoleId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetRole())
	updated, err := s.uc.UpdateRole(ctx, &biz.Role{
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
func (s *RoleService) DeleteRole(ctx context.Context, req *rolev1.DeleteRoleRequest) (*emptypb.Empty, error) {
	if err := s.uc.DeleteRole(ctx, req.GetRoleId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListRoleMenus returns the role's checkedKeys (granted menu ids shaped by
// menu_check_strictly).
func (s *RoleService) ListRoleMenus(ctx context.Context, req *rolev1.ListRoleMenusRequest) (*rolev1.RoleMenus, error) {
	ids, err := s.uc.ListRoleMenus(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	return &rolev1.RoleMenus{MenuIds: ids}, nil
}

// UpdateRoleMenus replaces the role's Menu Grant and menu_check_strictly in
// one transaction; the response echoes the persisted (deduped) grant.
func (s *RoleService) UpdateRoleMenus(ctx context.Context, req *rolev1.UpdateRoleMenusRequest) (*rolev1.RoleMenus, error) {
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
