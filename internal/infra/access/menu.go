package accessinfra

import (
	"glex/internal/infra/store"

	"context"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
)

// toBizMenu maps an ent Menu PO to a accessentity.Menu DO. Children is left nil — the
// tree is assembled in domain (see access.buildTree).
func toBizMenu(po *ent.Menu) *accessentity.Menu {
	return &accessentity.Menu{
		ID:         po.ID,
		MenuName:   po.MenuName,
		ParentID:   po.ParentID,
		OrderNum:   po.OrderNum,
		Path:       po.Path,
		Component:  po.Component,
		QueryParam: po.QueryParam,
		IsFrame:    int32(po.IsFrame),
		IsCache:    int32(po.IsCache),
		MenuType:   int32(po.MenuType),
		Visible:    int32(po.Visible),
		Status:     int32(po.Status),
		Perms:      po.Perms,
		Icon:       po.Icon,
		ActiveMenu: po.ActiveMenu,
		Ext:        po.Ext,
		CreateBy:   po.CreateBy,
		UpdateBy:   po.UpdateBy,
		CreatedAt:  po.CreatedAt,
		UpdatedAt:  po.UpdatedAt,
	}
}

type menuRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewMenuRepo creates a new MenuRepo instance.
func NewMenuRepo(data *store.Data) accessrepo.MenuRepo {
	return &menuRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.access.menu")),
	}
}

func (r *menuRepo) FindByID(ctx context.Context, id int64) (*accessentity.Menu, error) {
	po, err := r.data.DB.Menu(ctx).Query().
		Where(menu.IDEQ(id), menu.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrMenuNotFound
		}
		return nil, err
	}
	return toBizMenu(po), nil
}

func (r *menuRepo) FindByParentAndName(ctx context.Context, parentID int64, name string) (*accessentity.Menu, error) {
	po, err := r.data.DB.Menu(ctx).Query().
		Where(menu.ParentIDEQ(parentID), menu.MenuNameEQ(name), menu.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrMenuNotFound
		}
		return nil, err
	}
	return toBizMenu(po), nil
}

func (r *menuRepo) HasChildren(ctx context.Context, id int64) (bool, error) {
	n, err := r.data.DB.Menu(ctx).Query().
		Where(menu.ParentIDEQ(id), menu.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListMenus returns the non-deleted menus as a flat list. It does not apply
// offset/limit: menus are a small, tree-shaped set and ListMenus serves the
// whole tree. Final sibling ordering is applied by access.buildTree.
func (r *menuRepo) ListMenus(ctx context.Context, opts ...accessrepo.ListOption) ([]*accessentity.Menu, error) {
	o := accessrepo.ListOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.Menu(ctx).Query().
		Where(menu.DeletedAtIsNil(), ents.ApplyFilter(o.Filter)).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	menus := make([]*accessentity.Menu, 0, len(pos))
	for _, po := range pos {
		menus = append(menus, toBizMenu(po))
	}
	return menus, nil
}

// LoadRouteAccess freshly resolves the three database inputs for getRouters:
// active Role Assignments, their Menu Grants, and all active routeable Menus.
// It intentionally does not consult Functional Permission handles or an
// Account Session snapshot.
func (r *menuRepo) LoadRouteAccess(ctx context.Context, systemUserID int64) (*accessrepo.MenuRouteAccess, error) {
	assignedRoles, err := r.data.DB.Role(ctx).Query().
		Where(
			role.DeletedAtIsNil(),
			role.StatusEQ(0),
			role.HasUsersWith(
				user.IDEQ(systemUserID),
				user.DeletedAtIsNil(),
				user.StatusEQ(0),
			),
		).
		Order(role.ByRoleKey(), role.ByID()).
		All(ctx)
	if err != nil {
		return nil, routeAccessError(ctx, r.logger, "load_route_role_assignments", err)
	}

	roleKeys := make([]string, 0, len(assignedRoles))
	roleIDs := make([]int64, 0, len(assignedRoles))
	for _, assignedRole := range assignedRoles {
		roleKeys = append(roleKeys, assignedRole.RoleKey)
		roleIDs = append(roleIDs, assignedRole.ID)
	}

	grantedMenuIDs := make([]int64, 0)
	if len(roleIDs) > 0 {
		grantedMenuIDs, err = r.data.DB.Role(ctx).Query().
			Where(role.IDIn(roleIDs...)).
			QueryMenus().
			IDs(ctx)
		if err != nil {
			return nil, routeAccessError(ctx, r.logger, "load_route_menu_grants", err)
		}
	}

	pos, err := r.data.DB.Menu(ctx).Query().
		Where(
			menu.DeletedAtIsNil(),
			menu.StatusEQ(0),
			menu.MenuTypeIn(int8(accessentity.MenuTypeDirectory), int8(accessentity.MenuTypeMenu)),
		).
		Order(menu.ByParentID(), menu.ByOrderNum(), menu.ByID()).
		All(ctx)
	if err != nil {
		return nil, routeAccessError(ctx, r.logger, "load_active_route_menus", err)
	}
	menus := make([]*accessentity.Menu, 0, len(pos))
	for _, po := range pos {
		menus = append(menus, toBizMenu(po))
	}
	return &accessrepo.MenuRouteAccess{
		RoleKeys:       roleKeys,
		GrantedMenuIDs: grantedMenuIDs,
		ActiveMenus:    menus,
	}, nil
}

func (r *menuRepo) CreateMenu(ctx context.Context, m *accessentity.Menu) (*accessentity.Menu, error) {
	po, err := r.data.DB.Menu(ctx).Create().
		SetID(newSnowflakeID()).
		SetMenuName(m.MenuName).
		SetParentID(m.ParentID).
		SetOrderNum(m.OrderNum).
		SetPath(m.Path).
		SetComponent(m.Component).
		SetQueryParam(m.QueryParam).
		SetIsFrame(int8(m.IsFrame)).
		SetIsCache(int8(m.IsCache)).
		SetMenuType(int8(m.MenuType)).
		SetVisible(int8(m.Visible)).
		SetStatus(int8(m.Status)).
		SetPerms(m.Perms).
		SetIcon(m.Icon).
		SetActiveMenu(m.ActiveMenu).
		SetExt(m.Ext).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toBizMenu(po), nil
}

// UpdateMenu re-reads the record and its affected users, applies the mutation,
// and returns the persisted menu with its pre-image for MenuUsecase
// revocation classification. The caller owns the transaction.
func (r *menuRepo) UpdateMenu(ctx context.Context, m *accessentity.Menu) (*accessrepo.MenuUpdate, error) {
	result := &accessrepo.MenuUpdate{}
	current, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(m.ID), menu.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrMenuNotFound
		}
		return nil, err
	}
	roleIDs, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(m.ID)).QueryRoles().
		Where(role.DeletedAtIsNil()).IDs(ctx)
	if err != nil {
		return nil, err
	}
	userIDs, err := systemUserIDsForRoles(ctx, r.data.DB, roleIDs)
	if err != nil {
		return nil, err
	}
	po, err := r.data.DB.Menu(ctx).UpdateOneID(m.ID).
		SetMenuName(m.MenuName).
		SetParentID(m.ParentID).
		SetOrderNum(m.OrderNum).
		SetPath(m.Path).
		SetComponent(m.Component).
		SetQueryParam(m.QueryParam).
		SetIsFrame(int8(m.IsFrame)).
		SetIsCache(int8(m.IsCache)).
		SetMenuType(int8(m.MenuType)).
		SetVisible(int8(m.Visible)).
		SetStatus(int8(m.Status)).
		SetPerms(m.Perms).
		SetIcon(m.Icon).
		SetActiveMenu(m.ActiveMenu).
		SetExt(m.Ext).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	result.Menu = toBizMenu(po)
	result.Previous = toBizMenu(current)
	result.AffectedUserIDs = userIDs
	return result, nil
}

// ListByIDs returns the active menus among the given ids (soft-deleted rows
// are excluded, so the caller can detect unknown ids by length mismatch).
func (r *menuRepo) ListByIDs(ctx context.Context, ids []int64) ([]*accessentity.Menu, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	pos, err := r.data.DB.Menu(ctx).Query().
		Where(menu.IDIn(ids...), menu.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*accessentity.Menu, 0, len(pos))
	for _, po := range pos {
		out = append(out, toBizMenu(po))
	}
	return out, nil
}

// DeleteMenu soft-deletes the menu and hard-deletes its role links. It returns
// the System User identities observed before deletion; the caller owns the
// transaction and revocation ordering.
func (r *menuRepo) DeleteMenu(ctx context.Context, id int64) ([]int64, error) {
	roleIDs, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(id), menu.DeletedAtIsNil()).QueryRoles().
		Where(role.DeletedAtIsNil()).IDs(ctx)
	if err != nil {
		return nil, err
	}
	userIDs, err := systemUserIDsForRoles(ctx, r.data.DB, roleIDs)
	if err != nil {
		return nil, err
	}
	err = r.data.DB.Menu(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		ClearRoles().
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrMenuNotFound
		}
		return nil, err
	}
	return userIDs, nil
}

func systemUserIDsForRoles(ctx context.Context, db *ent.Database, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	return db.User(ctx).Query().Where(
		user.DeletedAtIsNil(),
		user.HasRolesWith(role.IDIn(roleIDs...)),
	).IDs(ctx)
}

// routeAccessError maps a database failure behind route resolution (which
// feeds the authenticated GetRouters endpoint) onto access's 503 error — the
// wire-identical twin of authn's AUTH_BACKEND_UNAVAILABLE — so no raw driver
// error crosses the seam and access depends on no authn type.
func routeAccessError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	logger.WarnContext(ctx, "route access database operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return access.ErrRouteAccessUnavailable
}
