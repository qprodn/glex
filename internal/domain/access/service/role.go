package service

import (
	"context"
	"errors"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
)

// ValidateRole rejects blank role_name/role_key (non-blank is the only rule).
func ValidateRole(r *entity.Role) error {
	if r.RoleName == "" || r.RoleKey == "" {
		return access.ErrRoleInvalid
	}
	return nil
}

// AssertRoleUnique checks role_name and role_key are not taken by another
// active role, ignoring the record with excludeID (0 = none). A not-found from
// the repo is the expected "available" case. Checks run against active rows
// only, so soft-deleted values are reusable.
func AssertRoleUnique(ctx context.Context, repo repository.RoleRepo, r *entity.Role, excludeID int64) error {
	if existing, err := repo.FindByRoleName(ctx, r.RoleName); err != nil {
		if !errors.Is(err, access.ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return access.ErrRoleNameConflict
	}
	if existing, err := repo.FindByRoleKey(ctx, r.RoleKey); err != nil {
		if !errors.Is(err, access.ErrRoleNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return access.ErrRoleKeyConflict
	}
	return nil
}

// AssertMenusGrantable dedupes menuIDs and checks every id exists and is not
// soft-deleted (the repo's ListByIDs filters deleted rows). An empty list is
// legal — it means "no grants" — and passes without touching the repo.
func AssertMenusGrantable(ctx context.Context, menuRepo repository.MenuRepo, menuIDs []int64) ([]int64, error) {
	unique := dedupeIDs(menuIDs)
	if len(unique) == 0 {
		return unique, nil
	}
	menus, err := menuRepo.ListByIDs(ctx, unique)
	if err != nil {
		return nil, err
	}
	if len(menus) != len(unique) {
		return nil, access.ErrRoleMenuNotFound
	}
	return unique, nil
}

// AssertRoleDisableAllowed rejects an update that *changes* the role's status
// to disabled (1) while users are still assigned. Re-disabling an already
// disabled role is a no-op and passes; other status directions are free.
func AssertRoleDisableAllowed(ctx context.Context, repo repository.RoleRepo, userRoleRepo repository.UserRoleRepo, r *entity.Role) error {
	if r.Status != 1 {
		return nil
	}
	existing, err := repo.FindByID(ctx, r.ID)
	if err != nil {
		return err
	}
	if existing.Status == 1 {
		return nil
	}
	n, err := userRoleRepo.CountUserRoles(ctx, r.ID)
	if err != nil {
		return err
	}
	if n > 0 {
		return access.ErrRoleAssigned
	}
	return nil
}

// CheckedMenuIDs shapes the persisted Menu Grant for the role editor. Under
// strict checking, linked parents of other linked menus are represented by
// their children and omitted from the returned checked keys.
func CheckedMenuIDs(role *entity.Role, links []*repository.RoleMenuLink) []int64 {
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
		return ids
	}
	for _, l := range links {
		ids = append(ids, l.MenuID)
	}
	return ids
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
