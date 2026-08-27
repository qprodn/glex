package access

import (
	"context"
	"log/slog"

	"glex/internal/crossdomain/authn"
	accessdomain "glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"
	accessservice "glex/internal/domain/access/service"
)

// RoleUsecase holds the role business rules.
type RoleUsecase struct {
	repo         accessrepo.RoleRepo
	menuRepo     accessrepo.MenuRepo
	userRoleRepo accessrepo.UserRoleRepo
	tx           Transaction
	revoker      authn.SessionRevoker
	logger       *slog.Logger
}

// NewRoleUsecase creates a RoleUsecase. menuRepo backs Menu Grant validation
// (granted menus must exist and not be soft-deleted); userRoleRepo backs the
// "a role with users cannot be deleted/disabled" guard (Role Assignment); tx
// wraps security writes together with their session revocation; revoker
// performs the revocation through the authn crossdomain contract.
func NewRoleUsecase(repo accessrepo.RoleRepo, menuRepo accessrepo.MenuRepo, userRoleRepo accessrepo.UserRoleRepo, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *RoleUsecase {
	return &RoleUsecase{
		repo:         repo,
		menuRepo:     menuRepo,
		userRoleRepo: userRoleRepo,
		tx:           tx,
		revoker:      revoker,
		logger:       logger.With(slog.String("component", "application.access.role")),
	}
}

// CreateRole validates and persists a role with its initial Menu Grant:
// role_name/role_key must be non-blank and unique among active roles, and
// every menu id must exist (duplicates silently deduped). The role row and
// its links commit in one transaction.
func (uc *RoleUsecase) CreateRole(ctx context.Context, r *accessentity.Role, menuIDs []int64) (*accessentity.Role, error) {
	if err := accessservice.ValidateRole(r); err != nil {
		return nil, err
	}
	if err := accessservice.AssertRoleUnique(ctx, uc.repo, r, 0); err != nil {
		return nil, err
	}
	unique, err := accessservice.AssertMenusGrantable(ctx, uc.menuRepo, menuIDs)
	if err != nil {
		return nil, err
	}
	var created *accessentity.Role
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = uc.repo.CreateRole(ctx, r, unique)
		return err
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateRole validates and persists a role, excluding the record being
// updated from the uniqueness checks. Menu Grant fields are untouched (the
// grant is replaced only via UpdateRoleMenus). Disabling a role that still
// has users assigned is rejected (RuoYi: 角色已分配，不能禁用). When the write
// invalidates Authorization Snapshots (role_key/status), assigned users'
// Login Sessions are currently revoked inside the same transaction. That
// failure behavior is not a stable business guarantee for Role updates.
func (uc *RoleUsecase) UpdateRole(ctx context.Context, r *accessentity.Role) (*accessentity.Role, error) {
	if err := accessservice.ValidateRole(r); err != nil {
		return nil, err
	}
	if err := accessservice.AssertRoleUnique(ctx, uc.repo, r, r.ID); err != nil {
		return nil, err
	}
	if err := accessservice.AssertRoleDisableAllowed(ctx, uc.repo, uc.userRoleRepo, r); err != nil {
		return nil, err
	}
	var updated *accessentity.Role
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := uc.repo.UpdateRole(ctx, r)
		if err != nil {
			return err
		}
		updated = result.Role
		if result.Role.AuthorizationChangedFrom(result.Previous) {
			return revokeAllSystemUsers(ctx, uc.revoker, result.AssignedUserIDs...)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// GetRole returns the role with the given id.
func (uc *RoleUsecase) GetRole(ctx context.Context, id int64) (*accessentity.Role, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListRoles lists roles matching the given options.
func (uc *RoleUsecase) ListRoles(ctx context.Context, opts ...accessrepo.ListOption) ([]*accessentity.Role, error) {
	return uc.repo.ListRoles(ctx, opts...)
}

// DeleteRole refuses to delete a role that still has users assigned (RuoYi:
// 已分配，不能删除), then soft-deletes it, removes its Menu Grant, and revokes
// the assigned users' Login Sessions in one transaction.
func (uc *RoleUsecase) DeleteRole(ctx context.Context, id int64) error {
	if _, err := uc.repo.FindByID(ctx, id); err != nil {
		return err
	}
	n, err := uc.userRoleRepo.CountUserRoles(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return accessdomain.ErrRoleAssigned
	}
	return uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.repo.DeleteRole(ctx, id)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.revoker, affected...)
	})
}

// ListRoleMenus returns the role's checkedKeys: every granted menu id
// (buttons included; soft-deleted excluded, repo-side) in (parent_id,
// order_num) order. When menu_check_strictly is on, a granted menu that is
// the parent of another granted menu is dropped — RuoYi semantics: half- and
// fully-checked parents show as tree state, not as grants (their children
// carry the grant).
func (uc *RoleUsecase) ListRoleMenus(ctx context.Context, roleID int64) ([]int64, error) {
	role, err := uc.repo.FindByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	links, err := uc.repo.ListRoleMenuLinks(ctx, roleID)
	if err != nil {
		return nil, err
	}
	return accessservice.CheckedMenuIDs(role, links), nil
}

// UpdateRoleMenus replaces the role's Menu Grant: menu_ids are validated
// (exist, not soft-deleted) and deduped, then stored together with
// menuCheckStrictly in one transaction (full replacement; an empty list
// clears all grants) that also revokes the assigned users' Login Sessions.
// Redis revocation failure rolling back the PostgreSQL replacement is part of
// this operation's business contract. Returns the persisted (deduped) ids.
func (uc *RoleUsecase) UpdateRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error) {
	if _, err := uc.repo.FindByID(ctx, roleID); err != nil {
		return nil, err
	}
	unique, err := accessservice.AssertMenusGrantable(ctx, uc.menuRepo, menuIDs)
	if err != nil {
		return nil, err
	}
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.repo.ReplaceRoleMenus(ctx, roleID, unique, menuCheckStrictly)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.revoker, affected...)
	}); err != nil {
		return nil, err
	}
	return unique, nil
}
