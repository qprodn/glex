package data

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
	"glex/internal/data/ent/role"
	"glex/internal/data/ent/user"
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
	data     *Data
	security *securityChangeCoordinator
}

// NewRoleRepo creates a new RoleRepo instance.
func NewRoleRepo(data *Data, security *securityChangeCoordinator) biz.RoleRepo {
	return &roleRepo{data: data, security: security}
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
	var po *ent.Role
	err := r.security.Run(ctx, func(change *securityChangeTx) error {
		if err := change.LockRoles(ctx, ro.ID); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		current, err := change.tx.Role.Query().Where(role.IDEQ(ro.ID), role.DeletedAtIsNil()).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		userIDs, err := change.tx.Role.Query().Where(role.IDEQ(ro.ID)).QueryUsers().
			Where(user.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockSystemUsers(ctx, userIDs...); err != nil {
			return err
		}
		po, err = change.tx.Role.UpdateOneID(ro.ID).
			SetRoleName(ro.RoleName).
			SetRoleKey(ro.RoleKey).
			SetRoleSort(ro.RoleSort).
			SetStatus(int8(ro.Status)).
			SetRemark(ro.Remark).
			Save(ctx)
		if err != nil {
			return err
		}
		if ro.AuthorizationChangedFrom(toBizRole(current)) {
			change.AffectSystemUsers(userIDs...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return toBizRole(po), nil
}

// DeleteRole soft-deletes the role and hard-deletes its Menu Grant in one
// transaction (RuoYi parity: a deleted role loses its grants for good).
func (r *roleRepo) DeleteRole(ctx context.Context, id int64) error {
	return r.security.Run(ctx, func(change *securityChangeTx) error {
		if err := change.LockMenuGrant(ctx, id); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		menuIDs, err := change.tx.Role.Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).QueryMenus().IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockMenus(ctx, menuIDs...); err != nil {
			return err
		}
		if err := change.LockRoles(ctx, id); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		if exists, err := change.tx.Role.Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return biz.ErrRoleNotFound
		}
		if _, err := change.tx.Role.Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).QueryMenus().IDs(ctx); err != nil {
			return err
		}
		userIDs, err := change.tx.Role.Query().Where(role.IDEQ(id)).QueryUsers().
			Where(user.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockSystemUsers(ctx, userIDs...); err != nil {
			return err
		}
		err = change.tx.Role.UpdateOneID(id).
			SetDeletedAt(time.Now()).
			ClearMenus().
			Exec(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		change.AffectSystemUsers(userIDs...)
		return nil
	})
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
	return r.security.Run(ctx, func(change *securityChangeTx) error {
		if err := change.LockMenuGrant(ctx, roleID); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		oldMenuIDs, err := change.tx.Role.Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).QueryMenus().IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockMenus(ctx, appendIDs(oldMenuIDs, menuIDs)...); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleMenuNotFound
			}
			return err
		}
		if err := change.LockRoles(ctx, roleID); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		if exists, err := change.tx.Role.Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return biz.ErrRoleNotFound
		}
		if _, err := change.tx.Role.Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).QueryMenus().IDs(ctx); err != nil {
			return err
		}
		if len(menuIDs) > 0 {
			count, err := change.tx.Menu.Query().Where(menu.IDIn(menuIDs...), menu.DeletedAtIsNil()).Count(ctx)
			if err != nil {
				return err
			}
			if count != len(sortedIDs(menuIDs)) {
				return biz.ErrRoleMenuNotFound
			}
		}
		userIDs, err := change.tx.Role.Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).QueryUsers().
			Where(user.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockSystemUsers(ctx, userIDs...); err != nil {
			return err
		}
		_, err = change.tx.Role.UpdateOneID(roleID).
			SetMenuCheckStrictly(menuCheckStrictly).
			ClearMenus().
			AddMenuIDs(menuIDs...).
			Save(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		change.AffectSystemUsers(userIDs...)
		return nil
	})
}
