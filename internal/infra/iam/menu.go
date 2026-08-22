package iaminfra

import (
	"glex/internal/infra/store"

	"context"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/iam"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
)

// toBizMenu maps an ent Menu PO to a iam.Menu DO. Children is left nil — the
// tree is assembled in domain (see iam.buildTree).
func toBizMenu(po *ent.Menu) *iam.Menu {
	return &iam.Menu{
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
func NewMenuRepo(data *store.Data) iam.MenuRepo {
	return &menuRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.iam.menu")),
	}
}

func (r *menuRepo) FindByID(ctx context.Context, id int64) (*iam.Menu, error) {
	po, err := r.data.DB.Menu(ctx).Query().
		Where(menu.IDEQ(id), menu.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, iam.ErrMenuNotFound
		}
		return nil, err
	}
	return toBizMenu(po), nil
}

func (r *menuRepo) FindByParentAndName(ctx context.Context, parentID int64, name string) (*iam.Menu, error) {
	po, err := r.data.DB.Menu(ctx).Query().
		Where(menu.ParentIDEQ(parentID), menu.MenuNameEQ(name), menu.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, iam.ErrMenuNotFound
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
// whole tree. Final sibling ordering is applied by iam.buildTree.
func (r *menuRepo) ListMenus(ctx context.Context, opts ...iam.ListOption) ([]*iam.Menu, error) {
	o := iam.ListOptions{}
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
	menus := make([]*iam.Menu, 0, len(pos))
	for _, po := range pos {
		menus = append(menus, toBizMenu(po))
	}
	return menus, nil
}

// LoadRouteAccess freshly resolves the three database inputs for getRouters:
// active Role Assignments, their Menu Grants, and all active routeable Menus.
// It intentionally does not consult Functional Permission handles or an
// Account Session snapshot.
func (r *menuRepo) LoadRouteAccess(ctx context.Context, systemUserID int64) (*iam.MenuRouteAccess, error) {
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
		return nil, authDatabaseBackendError(ctx, r.logger, "load_route_role_assignments", err)
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
			return nil, authDatabaseBackendError(ctx, r.logger, "load_route_menu_grants", err)
		}
	}

	pos, err := r.data.DB.Menu(ctx).Query().
		Where(
			menu.DeletedAtIsNil(),
			menu.StatusEQ(0),
			menu.MenuTypeIn(int8(iam.MenuTypeDirectory), int8(iam.MenuTypeMenu)),
		).
		Order(menu.ByParentID(), menu.ByOrderNum(), menu.ByID()).
		All(ctx)
	if err != nil {
		return nil, authDatabaseBackendError(ctx, r.logger, "load_active_route_menus", err)
	}
	menus := make([]*iam.Menu, 0, len(pos))
	for _, po := range pos {
		menus = append(menus, toBizMenu(po))
	}
	return &iam.MenuRouteAccess{
		RoleKeys:       roleKeys,
		GrantedMenuIDs: grantedMenuIDs,
		ActiveMenus:    menus,
	}, nil
}

func (r *menuRepo) CreateMenu(ctx context.Context, m *iam.Menu) (*iam.Menu, error) {
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

// UpdateMenu takes the Menu, Role, and System User authorization locks,
// re-reads the record and its affected users under them, applies the
// mutation, and returns the persisted menu with its pre-image and the
// affected users for the usecase's revocation classification. A security
// write — pair with SessionRevocation inside Transaction.InTx (see
// MenuUsecase.UpdateMenu).
func (r *menuRepo) UpdateMenu(ctx context.Context, m *iam.Menu) (*iam.MenuUpdate, error) {
	result := &iam.MenuUpdate{}
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		if err := lockMenus(ctx, r.data.DB, m.ID); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrMenuNotFound
			}
			return err
		}
		current, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(m.ID), menu.DeletedAtIsNil()).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrMenuNotFound
			}
			return err
		}
		roleIDs, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(m.ID)).QueryRoles().
			Where(role.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockRoles(ctx, r.data.DB, roleIDs...); err != nil {
			return err
		}
		userIDs, err := systemUserIDsForRoles(ctx, r.data.DB, roleIDs)
		if err != nil {
			return err
		}
		if err := lockSystemUsers(ctx, r.data.DB, userIDs...); err != nil {
			return err
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
			return err
		}
		result.Menu = toBizMenu(po)
		result.Previous = toBizMenu(current)
		result.AffectedUserIDs = userIDs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListByIDs returns the active menus among the given ids (soft-deleted rows
// are excluded, so the caller can detect unknown ids by length mismatch).
func (r *menuRepo) ListByIDs(ctx context.Context, ids []int64) ([]*iam.Menu, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	pos, err := r.data.DB.Menu(ctx).Query().
		Where(menu.IDIn(ids...), menu.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*iam.Menu, 0, len(pos))
	for _, po := range pos {
		out = append(out, toBizMenu(po))
	}
	return out, nil
}

// DeleteMenu soft-deletes the menu and hard-deletes its role links under the
// authorization locks: a deleted menu leaves no trace in sys_role_menu (RuoYi
// parity, so the link table never dangles). Returns the System User
// identities reaching the menu. A security write — pair with
// SessionRevocation inside Transaction.InTx.
func (r *menuRepo) DeleteMenu(ctx context.Context, id int64) ([]int64, error) {
	var affected []int64
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		if err := lockMenus(ctx, r.data.DB, id); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrMenuNotFound
			}
			return err
		}
		if exists, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(id), menu.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return iam.ErrMenuNotFound
		}
		roleIDs, err := r.data.DB.Menu(ctx).Query().Where(menu.IDEQ(id)).QueryRoles().
			Where(role.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockRoles(ctx, r.data.DB, roleIDs...); err != nil {
			return err
		}
		userIDs, err := systemUserIDsForRoles(ctx, r.data.DB, roleIDs)
		if err != nil {
			return err
		}
		if err := lockSystemUsers(ctx, r.data.DB, userIDs...); err != nil {
			return err
		}
		err = r.data.DB.Menu(ctx).UpdateOneID(id).
			SetDeletedAt(time.Now()).
			ClearRoles().
			Exec(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrMenuNotFound
			}
			return err
		}
		affected = userIDs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return affected, nil
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
