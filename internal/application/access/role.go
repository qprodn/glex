package access

import (
	"context"
	"log/slog"

	accessv1 "glex/api/access/v1"
	"glex/internal/crossdomain/authn"
	accessdomain "glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"
	accessservice "glex/internal/domain/access/service"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertRole maps a accessentity.Role DO to the proto Role DTO.
func convertRole(r *accessentity.Role) *accessv1.Role {
	return &accessv1.Role{
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

// RoleApplication implements the system-role CRUD service. It owns the
// module's cross-domain orchestration: the transaction boundary and session
// revocation through the authn crossdomain contract. Intra-domain rules
// (uniqueness, menu-grant validity, disable guards, checked-keys shaping)
// live in the domain service; authentication and the operation's Functional
// Permission policy are enforced by the registered HTTP middleware.
type RoleApplication struct {
	accessv1.UnimplementedRoleServiceServer

	repo         accessrepo.RoleRepo
	menuRepo     accessrepo.MenuRepo
	userRoleRepo accessrepo.UserRoleRepo
	tx           Transaction
	revoker      authn.SessionRevoker
	logger       *slog.Logger
}

// NewRoleApplication creates a RoleApplication. menuRepo backs Menu Grant
// validation (granted menus must exist and not be soft-deleted); userRoleRepo
// backs the "a role with users cannot be deleted/disabled" guard (Role
// Assignment); tx wraps security writes together with their session
// revocation; revoker performs the revocation through the authn crossdomain
// contract.
func NewRoleApplication(repo accessrepo.RoleRepo, menuRepo accessrepo.MenuRepo, userRoleRepo accessrepo.UserRoleRepo, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *RoleApplication {
	return &RoleApplication{
		repo:         repo,
		menuRepo:     menuRepo,
		userRoleRepo: userRoleRepo,
		tx:           tx,
		revoker:      revoker,
		logger:       logger.With(slog.String("component", "application.access.role")),
	}
}

// CreateRole creates a role with its optional initial Menu Grant. The
// presence-aware menu_check_strictly defaults to true (RuoYi default) when
// absent. role_name/role_key must be non-blank and unique among active roles,
// and every menu id must exist (duplicates silently deduped). The role row and
// its links commit in one transaction.
func (s *RoleApplication) CreateRole(ctx context.Context, req *accessv1.CreateRoleRequest) (*accessv1.Role, error) {
	in := req.GetRole()
	strict := true
	if req.MenuCheckStrictly != nil {
		strict = *req.MenuCheckStrictly
	}
	r := &accessentity.Role{
		RoleName:          in.GetRoleName(),
		RoleKey:           in.GetRoleKey(),
		RoleSort:          int(in.GetRoleSort()),
		Status:            in.GetStatus(),
		MenuCheckStrictly: strict,
		Remark:            in.GetRemark(),
	}
	actor := auditActor(ctx)
	r.CreateBy, r.UpdateBy = actor, actor
	if err := accessservice.ValidateRole(r); err != nil {
		return nil, err
	}
	if err := accessservice.AssertRoleUnique(ctx, s.repo, r, 0); err != nil {
		return nil, err
	}
	unique, err := accessservice.AssertMenusGrantable(ctx, s.menuRepo, req.GetMenuIds())
	if err != nil {
		return nil, err
	}
	var created *accessentity.Role
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.repo.CreateRole(ctx, r, unique)
		return err
	}); err != nil {
		return nil, err
	}
	return convertRole(created), nil
}

// GetRole retrieves a role by id.
func (s *RoleApplication) GetRole(ctx context.Context, req *accessv1.GetRoleRequest) (*accessv1.Role, error) {
	r, err := s.repo.FindByID(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	return convertRole(r), nil
}

// ListRoles lists roles with AIP-160 filter, AIP-132 order, and page-token
// pagination.
func (s *RoleApplication) ListRoles(ctx context.Context, req *accessv1.ListRolesRequest) (*accessv1.RoleSet, error) {
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
	roles, err := s.repo.ListRoles(ctx,
		accessrepo.ListFilter(filter),
		accessrepo.ListOrderBy(orderBy),
		accessrepo.ListLimit(int(req.GetPageSize())),
		accessrepo.ListOffset(int(pageToken.Offset)),
	)
	if err != nil {
		return nil, err
	}
	set := &accessv1.RoleSet{Roles: make([]*accessv1.Role, 0, len(roles))}
	for _, r := range roles {
		set.Roles = append(set.Roles, convertRole(r))
	}
	if len(roles) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// UpdateRole partially updates a role via field-mask. It loads the current
// state as a DTO, applies the mask onto it, then persists, excluding the
// record being updated from the uniqueness checks. Menu Grant fields are
// untouched (the grant is replaced only via UpdateRoleMenus). Disabling a role
// that still has users assigned is rejected (RuoYi: 角色已分配，不能禁用). When
// the write invalidates Authorization Snapshots (role_key/status), assigned
// users' Login Sessions are currently revoked inside the same transaction.
// That failure behavior is not a stable business guarantee for Role updates.
func (s *RoleApplication) UpdateRole(ctx context.Context, req *accessv1.UpdateRoleRequest) (*accessv1.Role, error) {
	current, err := s.GetRole(ctx, &accessv1.GetRoleRequest{RoleId: req.GetRole().GetRoleId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetRole())
	r := &accessentity.Role{
		ID:       current.GetRoleId(),
		RoleName: current.GetRoleName(),
		RoleKey:  current.GetRoleKey(),
		RoleSort: int(current.GetRoleSort()),
		Status:   current.GetStatus(),
		Remark:   current.GetRemark(),
		UpdateBy: auditActor(ctx),
	}
	if err := accessservice.ValidateRole(r); err != nil {
		return nil, err
	}
	if err := accessservice.AssertRoleUnique(ctx, s.repo, r, r.ID); err != nil {
		return nil, err
	}
	if err := accessservice.AssertRoleDisableAllowed(ctx, s.repo, s.userRoleRepo, r); err != nil {
		return nil, err
	}
	var updated *accessentity.Role
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := s.repo.UpdateRole(ctx, r)
		if err != nil {
			return err
		}
		updated = result.Role
		if result.Role.AuthorizationChangedFrom(result.Previous) {
			return revokeAllSystemUsers(ctx, s.revoker, result.AssignedUserIDs...)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return convertRole(updated), nil
}

// DeleteRole soft-deletes a role by id (and its Menu Grant, repo side);
// refused while users are still assigned (RuoYi: 已分配，不能删除). The delete
// revokes the assigned users' Login Sessions in one transaction.
func (s *RoleApplication) DeleteRole(ctx context.Context, req *accessv1.DeleteRoleRequest) (*emptypb.Empty, error) {
	if _, err := s.repo.FindByID(ctx, req.GetRoleId()); err != nil {
		return nil, err
	}
	n, err := s.userRoleRepo.CountUserRoles(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, accessdomain.ErrRoleAssigned
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := s.repo.DeleteRole(ctx, req.GetRoleId(), auditActor(ctx))
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, s.revoker, affected...)
	}); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListRoleMenus returns the role's checkedKeys (granted menu ids shaped by
// menu_check_strictly): every granted menu id (buttons included;
// soft-deleted excluded, repo-side) in (parent_id, order_num) order. When
// menu_check_strictly is on, a granted menu that is the parent of another
// granted menu is dropped — RuoYi semantics: half- and fully-checked parents
// show as tree state, not as grants (their children carry the grant).
func (s *RoleApplication) ListRoleMenus(ctx context.Context, req *accessv1.ListRoleMenusRequest) (*accessv1.RoleMenus, error) {
	role, err := s.repo.FindByID(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	links, err := s.repo.ListRoleMenuLinks(ctx, req.GetRoleId())
	if err != nil {
		return nil, err
	}
	return &accessv1.RoleMenus{MenuIds: accessservice.CheckedMenuIDs(role, links)}, nil
}

// UpdateRoleMenus replaces the role's Menu Grant and menu_check_strictly in
// one transaction; the response echoes the persisted (deduped) grant.
// menu_ids are validated (exist, not soft-deleted) and deduped, then stored
// (full replacement; an empty list clears all grants) together with
// menuCheckStrictly in one transaction that also revokes the assigned users'
// Login Sessions. Redis revocation failure rolling back the PostgreSQL
// replacement is part of this operation's business contract.
func (s *RoleApplication) UpdateRoleMenus(ctx context.Context, req *accessv1.UpdateRoleMenusRequest) (*accessv1.RoleMenus, error) {
	strict := true
	if req.MenuCheckStrictly != nil {
		strict = *req.MenuCheckStrictly
	}
	if _, err := s.repo.FindByID(ctx, req.GetRoleId()); err != nil {
		return nil, err
	}
	unique, err := accessservice.AssertMenusGrantable(ctx, s.menuRepo, req.GetMenuIds())
	if err != nil {
		return nil, err
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := s.repo.ReplaceRoleMenus(ctx, req.GetRoleId(), unique, strict, auditActor(ctx))
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, s.revoker, affected...)
	}); err != nil {
		return nil, err
	}
	return &accessv1.RoleMenus{MenuIds: unique}, nil
}
