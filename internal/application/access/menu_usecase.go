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

// MenuUsecase holds the menu business rules.
type MenuUsecase struct {
	repo    accessrepo.MenuRepo
	tx      Transaction
	revoker authn.SessionRevoker
	logger  *slog.Logger
}

// NewMenuUsecase creates a MenuUsecase. tx wraps security writes together
// with their session revocation; revoker performs the revocation through the
// authn crossdomain contract.
func NewMenuUsecase(repo accessrepo.MenuRepo, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *MenuUsecase {
	return &MenuUsecase{
		repo:    repo,
		tx:      tx,
		revoker: revoker,
		logger:  logger.With(slog.String("component", "application.access.menu")),
	}
}

// CreateMenu validates the type and sibling-name uniqueness, then persists.
// Uniqueness is defense-in-depth at the domain layer; there is no DB unique
// constraint on (parent_id, menu_name).
func (uc *MenuUsecase) CreateMenu(ctx context.Context, m *accessentity.Menu) (*accessentity.Menu, error) {
	if err := accessservice.ValidateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := accessservice.AssertSiblingNameUnique(ctx, uc.repo, m, false); err != nil {
		return nil, err
	}
	var created *accessentity.Menu
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = uc.repo.CreateMenu(ctx, m)
		return err
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// GetMenu returns the menu with the given id.
func (uc *MenuUsecase) GetMenu(ctx context.Context, id int64) (*accessentity.Menu, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListMenuTree loads the flat list and builds the menu tree in memory.
func (uc *MenuUsecase) ListMenuTree(ctx context.Context, opts ...accessrepo.ListOption) ([]*accessentity.Menu, error) {
	menus, err := uc.repo.ListMenus(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return accessservice.BuildMenuTree(menus), nil
}

// ListUserRouteTree builds the current System User's routes from fresh
// Role-Assignment/Menu-Grant data.
func (uc *MenuUsecase) ListUserRouteTree(ctx context.Context, systemUserID int64) ([]*accessentity.Menu, error) {
	routeAccess, err := uc.repo.LoadRouteAccess(ctx, systemUserID)
	if err != nil {
		return nil, err
	}
	return accessservice.SelectUserRouteTree(routeAccess), nil
}

// UpdateMenu rejects a self-parent, re-validates the type (so an explicit mask
// cannot zero it), and enforces sibling-name uniqueness excluding self, then
// persists. When the write invalidates Authorization Snapshots
// (perms/status), affected users' Login Sessions are revoked inside the same
// transaction. That failure behavior is an implementation detail for this
// operation rather than a stable business guarantee.
func (uc *MenuUsecase) UpdateMenu(ctx context.Context, m *accessentity.Menu) (*accessentity.Menu, error) {
	if m.ParentID == m.ID {
		return nil, accessdomain.ErrMenuParentInvalid
	}
	if err := accessservice.ValidateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := accessservice.AssertSiblingNameUnique(ctx, uc.repo, m, true); err != nil {
		return nil, err
	}
	var updated *accessentity.Menu
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := uc.repo.UpdateMenu(ctx, m)
		if err != nil {
			return err
		}
		updated = result.Menu
		if result.Menu.AuthorizationChangedFrom(result.Previous) {
			return revokeAllSystemUsers(ctx, uc.revoker, result.AffectedUserIDs...)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteMenu refuses to delete a menu that still has children, then
// soft-deletes it, removes its role links, and revokes affected users' Login
// Sessions in one transaction.
func (uc *MenuUsecase) DeleteMenu(ctx context.Context, id int64) error {
	has, err := uc.repo.HasChildren(ctx, id)
	if err != nil {
		return err
	}
	if has {
		return accessdomain.ErrMenuHasChildren
	}
	return uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.repo.DeleteMenu(ctx, id)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.revoker, affected...)
	})
}
