package iaminfra

import (
	"glex/internal/infra/store"

	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/iam"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
)

// toBizRole maps an ent Role PO to a iam.Role DO.
func toBizRole(po *ent.Role) *iam.Role {
	return &iam.Role{
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
	data *store.Data
}

// NewRoleRepo creates a new RoleRepo instance.
func NewRoleRepo(data *store.Data) iam.RoleRepo {
	return &roleRepo{data: data}
}

func (r *roleRepo) FindByID(ctx context.Context, id int64) (*iam.Role, error) {
	po, err := r.data.DB.Role(ctx).Query().
		Where(role.IDEQ(id), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, iam.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) FindByRoleName(ctx context.Context, roleName string) (*iam.Role, error) {
	po, err := r.data.DB.Role(ctx).Query().
		Where(role.RoleNameEQ(roleName), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, iam.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) FindByRoleKey(ctx context.Context, roleKey string) (*iam.Role, error) {
	po, err := r.data.DB.Role(ctx).Query().
		Where(role.RoleKeyEQ(roleKey), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, iam.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) ListRoles(ctx context.Context, opts ...iam.ListOption) ([]*iam.Role, error) {
	o := iam.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.Role(ctx).Query().
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
	roles := make([]*iam.Role, 0, len(pos))
	for _, po := range pos {
		roles = append(roles, toBizRole(po))
	}
	return roles, nil
}

// CreateRole inserts the role row and its initial Menu Grant (sys_role_menu
// rows) in one transaction (repo-internal; joins a caller transaction when
// one is already open).
func (r *roleRepo) CreateRole(ctx context.Context, ro *iam.Role, menuIDs []int64) (*iam.Role, error) {
	var po *ent.Role
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		var err error
		po, err = r.data.DB.Role(ctx).Create().
			SetID(newSnowflakeID()).
			SetRoleName(ro.RoleName).
			SetRoleKey(ro.RoleKey).
			SetRoleSort(ro.RoleSort).
			SetStatus(int8(ro.Status)).
			SetMenuCheckStrictly(ro.MenuCheckStrictly).
			SetRemark(ro.Remark).
			AddMenuIDs(menuIDs...).
			Save(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return toBizRole(po), nil
}

// UpdateRole takes the Role and System User authorization locks, re-reads the
// record and its assigned users under them, applies the mutation, and returns
// the persisted role with its pre-image and assigned users for the usecase's
// revocation classification. A security write — pair with SessionRevocation
// inside Transaction.InTx (see RoleUsecase.UpdateRole).
func (r *roleRepo) UpdateRole(ctx context.Context, ro *iam.Role) (*iam.RoleUpdate, error) {
	result := &iam.RoleUpdate{}
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		if err := lockRoles(ctx, r.data.DB, ro.ID); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		current, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(ro.ID), role.DeletedAtIsNil()).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		userIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(ro.ID)).QueryUsers().
			Where(user.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockSystemUsers(ctx, r.data.DB, userIDs...); err != nil {
			return err
		}
		po, err := r.data.DB.Role(ctx).UpdateOneID(ro.ID).
			SetRoleName(ro.RoleName).
			SetRoleKey(ro.RoleKey).
			SetRoleSort(ro.RoleSort).
			SetStatus(int8(ro.Status)).
			SetRemark(ro.Remark).
			Save(ctx)
		if err != nil {
			return err
		}
		result.Role = toBizRole(po)
		result.Previous = toBizRole(current)
		result.AssignedUserIDs = userIDs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteRole soft-deletes the role and hard-deletes its Menu Grant under the
// authorization locks (RuoYi parity: a deleted role loses its grants for
// good), returning the System User identities assigned the role at delete
// time. A security write — pair with SessionRevocation inside
// Transaction.InTx.
func (r *roleRepo) DeleteRole(ctx context.Context, id int64) ([]int64, error) {
	var affected []int64
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		if err := lockMenuGrant(ctx, r.data.DB, id); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		menuIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).QueryMenus().IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockMenus(ctx, r.data.DB, menuIDs...); err != nil {
			return err
		}
		if err := lockRoles(ctx, r.data.DB, id); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		if exists, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return iam.ErrRoleNotFound
		}
		if _, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).QueryMenus().IDs(ctx); err != nil {
			return err
		}
		userIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(id)).QueryUsers().
			Where(user.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockSystemUsers(ctx, r.data.DB, userIDs...); err != nil {
			return err
		}
		err = r.data.DB.Role(ctx).UpdateOneID(id).
			SetDeletedAt(time.Now()).
			ClearMenus().
			Exec(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
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

// ListRoleMenuLinks reads the role's granted menus (soft-deleted excluded)
// with their parent ids, ordered by (parent_id, order_num) — the ordering the
// role editor's tree expects.
func (r *roleRepo) ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*iam.RoleMenuLink, error) {
	pos, err := r.data.DB.Role(ctx).Query().
		Where(role.IDEQ(roleID), role.DeletedAtIsNil()).
		QueryMenus().
		Where(menu.DeletedAtIsNil()).
		Order(menu.ByParentID(), menu.ByOrderNum()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	links := make([]*iam.RoleMenuLink, 0, len(pos))
	for _, po := range pos {
		links = append(links, &iam.RoleMenuLink{MenuID: po.ID, ParentID: po.ParentID})
	}
	return links, nil
}

// ReplaceRoleMenus atomically replaces the role's Menu Grant (RuoYi's
// delete-then-insert) and writes menu_check_strictly under the authorization
// locks, returning the System User identities assigned the role. menuIDs are
// stored verbatim; the usecase validates and dedupes them first. A security
// write — pair with SessionRevocation inside Transaction.InTx.
func (r *roleRepo) ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error) {
	var affected []int64
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		if err := lockMenuGrant(ctx, r.data.DB, roleID); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		oldMenuIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).QueryMenus().IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockMenus(ctx, r.data.DB, appendIDs(oldMenuIDs, menuIDs)...); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleMenuNotFound
			}
			return err
		}
		if err := lockRoles(ctx, r.data.DB, roleID); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		if exists, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return iam.ErrRoleNotFound
		}
		if _, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).QueryMenus().IDs(ctx); err != nil {
			return err
		}
		if len(menuIDs) > 0 {
			count, err := r.data.DB.Menu(ctx).Query().Where(menu.IDIn(menuIDs...), menu.DeletedAtIsNil()).Count(ctx)
			if err != nil {
				return err
			}
			if count != len(sortedIDs(menuIDs)) {
				return iam.ErrRoleMenuNotFound
			}
		}
		userIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).QueryUsers().
			Where(user.DeletedAtIsNil()).IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockSystemUsers(ctx, r.data.DB, userIDs...); err != nil {
			return err
		}
		_, err = r.data.DB.Role(ctx).UpdateOneID(roleID).
			SetMenuCheckStrictly(menuCheckStrictly).
			ClearMenus().
			AddMenuIDs(menuIDs...).
			Save(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
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
