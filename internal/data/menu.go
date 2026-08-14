package data

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
)

// toBizMenu maps an ent Menu PO to a biz.Menu DO. Children is left nil — the
// tree is assembled in biz (see biz.buildTree).
func toBizMenu(po *ent.Menu) *biz.Menu {
	return &biz.Menu{
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
	data *Data
}

// NewMenuRepo creates a new MenuRepo instance.
func NewMenuRepo(data *Data) biz.MenuRepo {
	return &menuRepo{data: data}
}

func (r *menuRepo) FindByID(ctx context.Context, id int64) (*biz.Menu, error) {
	po, err := r.data.db.Menu.Query().
		Where(menu.IDEQ(id), menu.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrMenuNotFound
		}
		return nil, err
	}
	return toBizMenu(po), nil
}

func (r *menuRepo) FindByParentAndName(ctx context.Context, parentID int64, name string) (*biz.Menu, error) {
	po, err := r.data.db.Menu.Query().
		Where(menu.ParentIDEQ(parentID), menu.MenuNameEQ(name), menu.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrMenuNotFound
		}
		return nil, err
	}
	return toBizMenu(po), nil
}

func (r *menuRepo) HasChildren(ctx context.Context, id int64) (bool, error) {
	n, err := r.data.db.Menu.Query().
		Where(menu.ParentIDEQ(id), menu.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListMenus returns the non-deleted menus as a flat list. It does not apply
// offset/limit: menus are a small, tree-shaped set and ListMenus serves the
// whole tree. Final sibling ordering is applied by biz.buildTree.
func (r *menuRepo) ListMenus(ctx context.Context, opts ...biz.ListOption) ([]*biz.Menu, error) {
	o := biz.ListOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.db.Menu.Query().
		Where(menu.DeletedAtIsNil(), ents.ApplyFilter(o.Filter)).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	menus := make([]*biz.Menu, 0, len(pos))
	for _, po := range pos {
		menus = append(menus, toBizMenu(po))
	}
	return menus, nil
}

func (r *menuRepo) CreateMenu(ctx context.Context, m *biz.Menu) (*biz.Menu, error) {
	po, err := r.data.db.Menu.Create().
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

func (r *menuRepo) UpdateMenu(ctx context.Context, m *biz.Menu) (*biz.Menu, error) {
	po, err := r.data.db.Menu.UpdateOneID(m.ID).
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
		if ent.IsNotFound(err) {
			return nil, biz.ErrMenuNotFound
		}
		return nil, err
	}
	return toBizMenu(po), nil
}

// ListByIDs returns the active menus among the given ids (soft-deleted rows
// are excluded, so the caller can detect unknown ids by length mismatch).
func (r *menuRepo) ListByIDs(ctx context.Context, ids []int64) ([]*biz.Menu, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	pos, err := r.data.db.Menu.Query().
		Where(menu.IDIn(ids...), menu.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*biz.Menu, 0, len(pos))
	for _, po := range pos {
		out = append(out, toBizMenu(po))
	}
	return out, nil
}

// DeleteMenu soft-deletes the menu and hard-deletes its role links in one
// transaction: a deleted menu leaves no trace in sys_role_menu (RuoYi
// parity, so the link table never dangles).
func (r *menuRepo) DeleteMenu(ctx context.Context, id int64) error {
	tx, err := r.data.db.Tx(ctx)
	if err != nil {
		return err
	}
	err = tx.Menu.UpdateOneID(id).
		SetDeletedAt(time.Now()).
		ClearRoles().
		Exec(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return biz.ErrMenuNotFound
		}
		return err
	}
	return tx.Commit()
}
