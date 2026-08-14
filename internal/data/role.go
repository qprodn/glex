package data

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
	"glex/internal/data/ent/role"
)

// toBizRole maps an ent Role PO to a biz.Role DO.
func toBizRole(po *ent.Role) *biz.Role {
	return &biz.Role{
		ID:                po.ID,
		RoleName:          po.RoleName,
		RoleKey:           po.RoleKey,
		RoleSort:          po.RoleSort,
		Status:            int32(po.Status),
		MenuCheckStrictly: po.MenuCheckStrictly,
		Remark:            po.Remark,
		CreateBy:          po.CreateBy,
		UpdateBy:          po.UpdateBy,
		CreatedAt:         po.CreatedAt,
		UpdatedAt:         po.UpdatedAt,
	}
}

type roleRepo struct {
	data *Data
}

// NewRoleRepo creates a new RoleRepo instance.
func NewRoleRepo(data *Data) biz.RoleRepo {
	return &roleRepo{data: data}
}

func (r *roleRepo) FindByID(ctx context.Context, id int64) (*biz.Role, error) {
	po, err := r.data.db.Role.Query().
		Where(role.IDEQ(id), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) FindByRoleName(ctx context.Context, roleName string) (*biz.Role, error) {
	po, err := r.data.db.Role.Query().
		Where(role.RoleNameEQ(roleName), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) FindByRoleKey(ctx context.Context, roleKey string) (*biz.Role, error) {
	po, err := r.data.db.Role.Query().
		Where(role.RoleKeyEQ(roleKey), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) ListRoles(ctx context.Context, opts ...biz.ListOption) ([]*biz.Role, error) {
	o := biz.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.db.Role.Query().
		Where(
			role.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	roles := make([]*biz.Role, 0, len(pos))
	for _, po := range pos {
		roles = append(roles, toBizRole(po))
	}
	return roles, nil
}

// CreateRole inserts the role row and its initial Menu Grant (sys_role_menu
// rows) in one transaction.
func (r *roleRepo) CreateRole(ctx context.Context, ro *biz.Role, menuIDs []int64) (*biz.Role, error) {
	tx, err := r.data.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	po, err := tx.Role.Create().
		SetID(newSnowflakeID()).
		SetRoleName(ro.RoleName).
		SetRoleKey(ro.RoleKey).
		SetRoleSort(ro.RoleSort).
		SetStatus(int8(ro.Status)).
		SetMenuCheckStrictly(ro.MenuCheckStrictly).
		SetRemark(ro.Remark).
		AddMenuIDs(menuIDs...).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) UpdateRole(ctx context.Context, ro *biz.Role) (*biz.Role, error) {
	po, err := r.data.db.Role.UpdateOneID(ro.ID).
		SetRoleName(ro.RoleName).
		SetRoleKey(ro.RoleKey).
		SetRoleSort(ro.RoleSort).
		SetStatus(int8(ro.Status)).
		SetRemark(ro.Remark).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

// DeleteRole soft-deletes the role and hard-deletes its Menu Grant in one
// transaction (RuoYi parity: a deleted role loses its grants for good).
func (r *roleRepo) DeleteRole(ctx context.Context, id int64) error {
	tx, err := r.data.db.Tx(ctx)
	if err != nil {
		return err
	}
	err = tx.Role.UpdateOneID(id).
		SetDeletedAt(time.Now()).
		ClearMenus().
		Exec(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return biz.ErrRoleNotFound
		}
		return err
	}
	return tx.Commit()
}

// ListRoleMenuLinks reads the role's granted menus (soft-deleted excluded)
// with their parent ids, ordered by (parent_id, order_num) — the ordering the
// role editor's tree expects.
func (r *roleRepo) ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*biz.RoleMenuLink, error) {
	pos, err := r.data.db.Role.Query().
		Where(role.IDEQ(roleID), role.DeletedAtIsNil()).
		QueryMenus().
		Where(menu.DeletedAtIsNil()).
		Order(menu.ByParentID(), menu.ByOrderNum()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	links := make([]*biz.RoleMenuLink, 0, len(pos))
	for _, po := range pos {
		links = append(links, &biz.RoleMenuLink{MenuID: po.ID, ParentID: po.ParentID})
	}
	return links, nil
}

// ReplaceRoleMenus atomically replaces the role's Menu Grant (RuoYi's
// delete-then-insert) and writes menu_check_strictly in one transaction.
// menuIDs are stored verbatim; the usecase validates and dedupes them first.
func (r *roleRepo) ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) error {
	tx, err := r.data.db.Tx(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Role.UpdateOneID(roleID).
		SetMenuCheckStrictly(menuCheckStrictly).
		ClearMenus().
		AddMenuIDs(menuIDs...).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return biz.ErrRoleNotFound
		}
		return err
	}
	return tx.Commit()
}
