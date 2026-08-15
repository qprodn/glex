package biz

import (
	"context"
	"errors"
	"time"
)

// Role is a role domain object (角色), ported from RuoYi sys_role.
// Pure biz model: no proto, no storage tags. Carries a Menu Grant (菜单授权)
// via the repo; user↔role assignment is deferred to the auth module.
type Role struct {
	ID                int64
	RoleName          string
	RoleKey           string
	RoleSort          int
	Status            int32
	MenuCheckStrictly bool
	Remark            string
	CreateBy          int64
	UpdateBy          int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// RoleMenuLink is one row of a role's Menu Grant as read from storage: the
// granted menu id and its parent id. The read path uses ParentID to shape
// checkedKeys (see RoleUsecase.ListRoleMenus).
type RoleMenuLink struct {
	MenuID   int64
	ParentID int64
}

// RoleRepo is the repository interface for roles (the inversion seam declared
// in biz, implemented in data).
type RoleRepo interface {
	FindByID(ctx context.Context, id int64) (*Role, error)
	FindByRoleName(ctx context.Context, roleName string) (*Role, error)
	FindByRoleKey(ctx context.Context, roleKey string) (*Role, error)
	ListRoles(ctx context.Context, opts ...ListOption) ([]*Role, error)
	// CreateRole persists the role and its initial Menu Grant in one
	// transaction. menuIDs are stored verbatim (deduped and validated by the
	// usecase first).
	CreateRole(ctx context.Context, r *Role, menuIDs []int64) (*Role, error)
	UpdateRole(ctx context.Context, r *Role) (*Role, error)
	// DeleteRole soft-deletes the role and hard-deletes its Menu Grant in one
	// transaction (RuoYi semantics: a deleted role loses its grants for good).
	DeleteRole(ctx context.Context, id int64) error
	// ListRoleMenuLinks returns the role's granted menus with their parent
	// ids, ordered by (parent_id, order_num); soft-deleted menus excluded.
	ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*RoleMenuLink, error)
	// ReplaceRoleMenus atomically replaces the role's Menu Grant with menuIDs
	// and writes menuCheckStrictly (full replacement — RuoYi's
	// delete-then-insert).
	ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) error
}

// RoleUsecase holds the role business rules.
type RoleUsecase struct {
	repo         RoleRepo
	menuRepo     MenuRepo
	userRoleRepo UserRoleRepo
}

// NewRoleUsecase creates a RoleUsecase. menuRepo backs Menu Grant validation
// (granted menus must exist and not be soft-deleted); userRoleRepo backs the
// "a role with users cannot be deleted/disabled" guard (Role Assignment).
func NewRoleUsecase(repo RoleRepo, menuRepo MenuRepo, userRoleRepo UserRoleRepo) *RoleUsecase {
	return &RoleUsecase{repo: repo, menuRepo: menuRepo, userRoleRepo: userRoleRepo}
}

// CreateRole validates and persists a role with its initial Menu Grant:
// role_name/role_key must be non-blank and unique among active roles, and
// every menu id must exist (duplicates silently deduped). The role row and
// its links commit in one transaction.
func (uc *RoleUsecase) CreateRole(ctx context.Context, r *Role, menuIDs []int64) (*Role, error) {
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
	return uc.repo.CreateRole(ctx, r, unique)
}

// UpdateRole validates and persists a role, excluding the record being
// updated from the uniqueness checks. Menu Grant fields are untouched (the
// grant is replaced only via UpdateRoleMenus). Disabling a role that still
// has users assigned is rejected (RuoYi: 角色已分配，不能禁用).
func (uc *RoleUsecase) UpdateRole(ctx context.Context, r *Role) (*Role, error) {
	if err := uc.validate(r); err != nil {
		return nil, err
	}
	if err := uc.assertUnique(ctx, r, r.ID); err != nil {
		return nil, err
	}
	if err := uc.assertRoleDisableAllowed(ctx, r); err != nil {
		return nil, err
	}
	return uc.repo.UpdateRole(ctx, r)
}

// GetRole returns the role with the given id.
func (uc *RoleUsecase) GetRole(ctx context.Context, id int64) (*Role, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListRoles lists roles matching the given options.
func (uc *RoleUsecase) ListRoles(ctx context.Context, opts ...ListOption) ([]*Role, error) {
	return uc.repo.ListRoles(ctx, opts...)
}

// DeleteRole refuses to delete a role that still has users assigned (RuoYi:
// 已分配，不能删除), then soft-deletes it and removes its Menu Grant (single
// transaction, repo side).
func (uc *RoleUsecase) DeleteRole(ctx context.Context, id int64) error {
	if _, err := uc.repo.FindByID(ctx, id); err != nil {
		return err
	}
	n, err := uc.userRoleRepo.CountUserRoles(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrRoleAssigned
	}
	return uc.repo.DeleteRole(ctx, id)
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
// clears all grants). Returns the persisted (deduped) ids.
func (uc *RoleUsecase) UpdateRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error) {
	if _, err := uc.repo.FindByID(ctx, roleID); err != nil {
		return nil, err
	}
	unique, err := uc.assertMenusGrantable(ctx, menuIDs)
	if err != nil {
		return nil, err
	}
	if err := uc.repo.ReplaceRoleMenus(ctx, roleID, unique, menuCheckStrictly); err != nil {
		return nil, err
	}
	return unique, nil
}

// validate rejects blank role_name/role_key (Q4: non-blank is the only rule).
func (uc *RoleUsecase) validate(r *Role) error {
	if r.RoleName == "" || r.RoleKey == "" {
		return ErrRoleInvalid
	}
	return nil
}

// assertUnique checks role_name and role_key are not taken by another active
// role, ignoring the record with excludeID (0 = none). A not-found from the
// repo is the expected "available" case. Checks run against active rows only,
// so soft-deleted values are reusable.
func (uc *RoleUsecase) assertUnique(ctx context.Context, r *Role, excludeID int64) error {
	if existing, err := uc.repo.FindByRoleName(ctx, r.RoleName); err != nil {
		if !errors.Is(err, ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return ErrRoleNameConflict
	}
	if existing, err := uc.repo.FindByRoleKey(ctx, r.RoleKey); err != nil {
		if !errors.Is(err, ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return ErrRoleKeyConflict
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
		return nil, ErrRoleMenuNotFound
	}
	return unique, nil
}

// assertRoleDisableAllowed rejects an update that *changes* the role's status
// to disabled (1) while users are still assigned. Re-disabling an already
// disabled role is a no-op and passes; other status directions are free.
func (uc *RoleUsecase) assertRoleDisableAllowed(ctx context.Context, r *Role) error {
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
		return ErrRoleAssigned
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
