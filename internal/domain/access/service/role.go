package service

import (
	"context"
	"errors"
	"log/slog"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// Role is the access domain service behind the role endpoints: the only path
// to the Role, Menu Grant, and assignment-guard reads and writes (ADR 0005).
// Methods join the caller's ambient transaction; the Application service
// owns the transaction boundary and the cross-domain revocation. Every error
// a method returns to its caller first logs its Warn breadcrumb (ADR 0011).
type Role interface {
	Get(ctx context.Context, id int64) (*entity.Role, error)
	List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.Role, error)
	// Create validates the role and its initial Menu Grant and persists both.
	Create(ctx context.Context, r *entity.Role, menuIDs []int64) (*entity.Role, error)
	// Update validates the change (shape, uniqueness, disable guard) and
	// persists it, returning the write with its pre-image and assigned users.
	Update(ctx context.Context, r *entity.Role) (*entity.RoleUpdate, error)
	// Delete soft-deletes the role and its Menu Grant, refusing while users
	// are still assigned, and returns the assigned identities.
	Delete(ctx context.Context, id, actorID int64) ([]int64, error)
	// CheckedMenuIDs returns the role's granted menu ids shaped by
	// menu_check_strictly (the role editor's checked keys).
	CheckedMenuIDs(ctx context.Context, roleID int64) ([]int64, error)
	// ReplaceMenus validates and dedupes menuIDs, replaces the role's Menu
	// Grant together with menu_check_strictly, and returns the stored grant
	// with the assigned identities whose snapshots the write invalidates.
	ReplaceMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool, actorID int64) (granted []int64, affectedUserIDs []int64, err error)
}

type roleImpl struct {
	roles     repository.RoleRepo
	menus     repository.MenuRepo
	userRoles repository.UserRoleRepo
	logger    *slog.Logger
}

// NewRoleService creates the role domain service. menus backs Menu Grant
// validation; userRoles backs the "a role with users cannot be deleted or
// disabled" guard.
func NewRoleService(roles repository.RoleRepo, menus repository.MenuRepo, userRoles repository.UserRoleRepo, logger *slog.Logger) Role {
	return &roleImpl{
		roles:     roles,
		menus:     menus,
		userRoles: userRoles,
		logger:    logger.With(slog.String("component", "domain.access.svc")),
	}
}

func (s *roleImpl) Get(ctx context.Context, id int64) (*entity.Role, error) {
	r, err := s.roles.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.get_role", err)
		return nil, err
	}
	return r, nil
}

func (s *roleImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.Role, error) {
	rs, err := s.roles.ListRoles(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.list_roles", err)
		return nil, err
	}
	return rs, nil
}

func (s *roleImpl) Create(ctx context.Context, r *entity.Role, menuIDs []int64) (*entity.Role, error) {
	if err := ValidateRole(r); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.create_role", err)
		return nil, err
	}
	if err := AssertRoleUnique(ctx, s.roles, r, 0); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.create_role", err)
		return nil, err
	}
	unique, err := AssertMenusGrantable(ctx, s.menus, menuIDs)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.create_role", err)
		return nil, err
	}
	created, err := s.roles.CreateRole(ctx, r, unique)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.create_role", err)
		return nil, err
	}
	return created, nil
}

func (s *roleImpl) Update(ctx context.Context, r *entity.Role) (*entity.RoleUpdate, error) {
	if err := ValidateRole(r); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.update_role", err)
		return nil, err
	}
	if err := AssertRoleUnique(ctx, s.roles, r, r.ID); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.update_role", err)
		return nil, err
	}
	if err := AssertRoleDisableAllowed(ctx, s.roles, s.userRoles, r); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.update_role", err)
		return nil, err
	}
	updated, err := s.roles.UpdateRole(ctx, r)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.update_role", err)
		return nil, err
	}
	return updated, nil
}

func (s *roleImpl) Delete(ctx context.Context, id, actorID int64) ([]int64, error) {
	if _, err := s.roles.FindByID(ctx, id); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.delete_role", err)
		return nil, err
	}
	n, err := s.userRoles.CountUserRoles(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.delete_role", err)
		return nil, err
	}
	if n > 0 {
		err := access.ErrRoleAssigned
		logx.Propagate(ctx, s.logger, "access.svc.delete_role", err)
		return nil, err
	}
	revoked, err := s.roles.DeleteRole(ctx, id, actorID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.delete_role", err)
		return nil, err
	}
	return revoked, nil
}

func (s *roleImpl) CheckedMenuIDs(ctx context.Context, roleID int64) ([]int64, error) {
	role, err := s.roles.FindByID(ctx, roleID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.checked_menu_ids", err)
		return nil, err
	}
	links, err := s.roles.ListRoleMenuLinks(ctx, roleID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.checked_menu_ids", err)
		return nil, err
	}
	return CheckedMenuIDs(role, links), nil
}

func (s *roleImpl) ReplaceMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool, actorID int64) ([]int64, []int64, error) {
	if _, err := s.roles.FindByID(ctx, roleID); err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_menus", err)
		return nil, nil, err
	}
	unique, err := AssertMenusGrantable(ctx, s.menus, menuIDs)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_menus", err)
		return nil, nil, err
	}
	affected, err := s.roles.ReplaceRoleMenus(ctx, roleID, unique, menuCheckStrictly, actorID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.replace_menus", err)
		return nil, nil, err
	}
	return unique, affected, nil
}

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
// only, so soft-deleted values are reusable. Its caller's method boundary
// owns the breadcrumb (ADR 0011).
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
// legal — it means "no grants" — and passes without touching the repo. Its
// caller's method boundary owns the breadcrumb (ADR 0011).
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
// disabled role is a no-op and passes; other status directions are free. Its
// caller's method boundary owns the breadcrumb (ADR 0011).
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
