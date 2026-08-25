package iam

import (
	"context"
	"errors"
	"log/slog"

	iamdomain "glex/internal/domain/iam"
)

// RoleUsecase holds the role business rules.
type RoleUsecase struct {
	repo         iamdomain.RoleRepo
	menuRepo     iamdomain.MenuRepo
	userRoleRepo iamdomain.UserRoleRepo
	tx           Transaction
	revoker      iamdomain.SessionRevoker
	logger       *slog.Logger
}

// NewRoleUsecase creates a RoleUsecase. menuRepo backs Menu Grant validation
// (granted menus must exist and not be soft-deleted); userRoleRepo backs the
// "a role with users cannot be deleted/disabled" guard (iamdomain.Role Assignment); tx
// wraps security writes together with their session revocation; revoker
// performs the revocation.
func NewRoleUsecase(repo iamdomain.RoleRepo, menuRepo iamdomain.MenuRepo, userRoleRepo iamdomain.UserRoleRepo, tx Transaction, revoker iamdomain.SessionRevoker, logger *slog.Logger) *RoleUsecase {
	return &RoleUsecase{
		repo:         repo,
		menuRepo:     menuRepo,
		userRoleRepo: userRoleRepo,
		tx:           tx,
		revoker:      revoker,
		logger:       logger.With(slog.String("component", "application.iam.role")),
	}
}

// CreateRole validates and persists a role with its initial Menu Grant:
// role_name/role_key must be non-blank and unique among active roles, and
// every menu id must exist (duplicates silently deduped). The role row and
// its links commit in one transaction.
func (uc *RoleUsecase) CreateRole(ctx context.Context, r *iamdomain.Role, menuIDs []int64) (*iamdomain.Role, error) {
	if err := uc.validate(r); err != nil {
		return nil, err
	}
	if err := uc.assertUnique(ctx, r, 0); err != nil {
		return nil, err
	}
	unique, err := uc.assertMenusGrantable(ctx, menuIDs)
	if err != nil {
		return nil, err
	}
	var created *iamdomain.Role
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
func (uc *RoleUsecase) UpdateRole(ctx context.Context, r *iamdomain.Role) (*iamdomain.Role, error) {
	if err := uc.validate(r); err != nil {
		return nil, err
	}
	if err := uc.assertUnique(ctx, r, r.ID); err != nil {
		return nil, err
	}
	if err := uc.assertRoleDisableAllowed(ctx, r); err != nil {
		return nil, err
	}
	var updated *iamdomain.Role
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
func (uc *RoleUsecase) GetRole(ctx context.Context, id int64) (*iamdomain.Role, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListRoles lists roles matching the given options.
func (uc *RoleUsecase) ListRoles(ctx context.Context, opts ...iamdomain.ListOption) ([]*iamdomain.Role, error) {
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
		return iamdomain.ErrRoleAssigned
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
	ids := make([]int64, 0, len(links))
	if role.MenuCheckStrictly {
		parentOf := make(map[int64]struct{}, len(links))
		for _, l := range links {
			if l.ParentID != 0 {
				parentOf[l.ParentID] = struct{}{}
			}
		}
		for _, l := range links {
			if _, half := parentOf[l.MenuID]; half {
				continue
			}
			ids = append(ids, l.MenuID)
		}
		return ids, nil
	}
	for _, l := range links {
		ids = append(ids, l.MenuID)
	}
	return ids, nil
}

// UpdateRoleMenus replaces the role's Menu Grant: menu_ids are validated
// (exist, not soft-deleted) and deduped, then stored together with
// menuCheckStrictly in one transaction (full replacement; an empty list
// clears all grants) that also revokes the assigned users' Login Sessions.
// Redis revocation failure rolling back the PostgreSQL replacement is part of
// this Usecase's business contract. Returns the persisted (deduped) ids.
func (uc *RoleUsecase) UpdateRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error) {
	if _, err := uc.repo.FindByID(ctx, roleID); err != nil {
		return nil, err
	}
	unique, err := uc.assertMenusGrantable(ctx, menuIDs)
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

// validate rejects blank role_name/role_key (Q4: non-blank is the only rule).
func (uc *RoleUsecase) validate(r *iamdomain.Role) error {
	if r.RoleName == "" || r.RoleKey == "" {
		return iamdomain.ErrRoleInvalid
	}
	return nil
}

// assertUnique checks role_name and role_key are not taken by another active
// role, ignoring the record with excludeID (0 = none). A not-found from the
// repo is the expected "available" case. Checks run against active rows only,
// so soft-deleted values are reusable.
func (uc *RoleUsecase) assertUnique(ctx context.Context, r *iamdomain.Role, excludeID int64) error {
	if existing, err := uc.repo.FindByRoleName(ctx, r.RoleName); err != nil {
		if !errors.Is(err, iamdomain.ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return iamdomain.ErrRoleNameConflict
	}
	if existing, err := uc.repo.FindByRoleKey(ctx, r.RoleKey); err != nil {
		if !errors.Is(err, iamdomain.ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return iamdomain.ErrRoleKeyConflict
	}
	return nil
}

// assertMenusGrantable dedupes menuIDs and checks every id exists and is not
// soft-deleted (the repo's ListByIDs filters deleted rows). An empty list is
// legal — it means "no grants" — and passes without touching the repo.
func (uc *RoleUsecase) assertMenusGrantable(ctx context.Context, menuIDs []int64) ([]int64, error) {
	unique := dedupeIDs(menuIDs)
	if len(unique) == 0 {
		return unique, nil
	}
	menus, err := uc.menuRepo.ListByIDs(ctx, unique)
	if err != nil {
		return nil, err
	}
	if len(menus) != len(unique) {
		return nil, iamdomain.ErrRoleMenuNotFound
	}
	return unique, nil
}

// assertRoleDisableAllowed rejects an update that *changes* the role's status
// to disabled (1) while users are still assigned. Re-disabling an already
// disabled role is a no-op and passes; other status directions are free.
func (uc *RoleUsecase) assertRoleDisableAllowed(ctx context.Context, r *iamdomain.Role) error {
	if r.Status != 1 {
		return nil
	}
	existing, err := uc.repo.FindByID(ctx, r.ID)
	if err != nil {
		return err
	}
	if existing.Status == 1 {
		return nil
	}
	n, err := uc.userRoleRepo.CountUserRoles(ctx, r.ID)
	if err != nil {
		return err
	}
	if n > 0 {
		return iamdomain.ErrRoleAssigned
	}
	return nil
}

// dedupeIDs removes duplicates, preserving first-occurrence order.
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
