package accessinfra

import (
	"glex/internal/infra/store"

	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/rolemenu"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/ent/userrole"
)

// toBizRole maps an ent Role PO to a accessentity.Role DO.
func toBizRole(po *ent.Role) *accessentity.Role {
	return &accessentity.Role{
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
func NewRoleRepo(data *store.Data) accessrepo.RoleRepo {
	return &roleRepo{data: data}
}

func (r *roleRepo) FindByID(ctx context.Context, id int64) (*accessentity.Role, error) {
	po, err := r.data.DB.Role(ctx).Query().
		Where(role.IDEQ(id), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) FindByRoleName(ctx context.Context, roleName string) (*accessentity.Role, error) {
	po, err := r.data.DB.Role(ctx).Query().
		Where(role.RoleNameEQ(roleName), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) FindByRoleKey(ctx context.Context, roleKey string) (*accessentity.Role, error) {
	po, err := r.data.DB.Role(ctx).Query().
		Where(role.RoleKeyEQ(roleKey), role.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrRoleNotFound
		}
		return nil, err
	}
	return toBizRole(po), nil
}

func (r *roleRepo) ListRoles(ctx context.Context, opts ...accessrepo.ListOption) ([]*accessentity.Role, error) {
	o := accessrepo.ListOptions{Limit: 20}
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
	roles := make([]*accessentity.Role, 0, len(pos))
	for _, po := range pos {
		roles = append(roles, toBizRole(po))
	}
	return roles, nil
}

// CreateRole inserts the role row and its initial Menu Grant (link rows
// stamped with the actor from the DO's audit fields). The caller owns the
// transaction.
func (r *roleRepo) CreateRole(ctx context.Context, ro *accessentity.Role, menuIDs []int64) (*accessentity.Role, error) {
	po, err := r.data.DB.Role(ctx).Create().
		SetID(newSnowflakeID()).
		SetRoleName(ro.RoleName).
		SetRoleKey(ro.RoleKey).
		SetRoleSort(ro.RoleSort).
		SetStatus(int8(ro.Status)).
		SetMenuCheckStrictly(ro.MenuCheckStrictly).
		SetRemark(ro.Remark).
		SetCreateBy(ro.CreateBy).
		SetUpdateBy(ro.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := insertMenuGrants(ctx, r.data, ro.ID, menuIDs, ro.CreateBy); err != nil {
		return nil, err
	}
	return toBizRole(po), nil
}

// UpdateRole reads the record and assigned users, applies the mutation, and
// returns both snapshots for RoleUsecase revocation classification. The
// caller owns the transaction.
func (r *roleRepo) UpdateRole(ctx context.Context, ro *accessentity.Role) (*accessentity.RoleUpdate, error) {
	result := &accessentity.RoleUpdate{}
	current, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(ro.ID), role.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrRoleNotFound
		}
		return nil, err
	}
	userIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(ro.ID)).
		QueryUserLinks().Where(userrole.DeletedAtIsNil()).
		QueryUser().Where(user.DeletedAtIsNil()).IDs(ctx)
	if err != nil {
		return nil, err
	}
	po, err := r.data.DB.Role(ctx).UpdateOneID(ro.ID).
		SetRoleName(ro.RoleName).
		SetRoleKey(ro.RoleKey).
		SetRoleSort(ro.RoleSort).
		SetStatus(int8(ro.Status)).
		SetRemark(ro.Remark).
		SetUpdateBy(ro.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	result.Role = toBizRole(po)
	result.Previous = toBizRole(current)
	result.AssignedUserIDs = userIDs
	return result, nil
}

// DeleteRole soft-deletes the role and its Menu Grant (link rows stamped with
// actorID — ADR 0002, nothing is hard-deleted), returning the System User
// identities observed before deletion. The caller owns the transaction and
// revocation ordering.
func (r *roleRepo) DeleteRole(ctx context.Context, id int64, actorID int64) ([]int64, error) {
	userIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(id), role.DeletedAtIsNil()).
		QueryUserLinks().Where(userrole.DeletedAtIsNil()).
		QueryUser().Where(user.DeletedAtIsNil()).IDs(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := r.data.DB.RoleMenu(ctx).Update().
		Where(rolemenu.RoleIDEQ(id), rolemenu.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Save(ctx); err != nil {
		return nil, err
	}
	err = r.data.DB.Role(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrRoleNotFound
		}
		return nil, err
	}
	return userIDs, nil
}

// ListRoleMenuLinks reads the role's granted menus (soft-deleted links and
// menus excluded) with their parent ids, ordered by (parent_id, order_num) —
// the ordering the role editor's tree expects.
func (r *roleRepo) ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*accessrepo.RoleMenuLink, error) {
	pos, err := r.data.DB.Role(ctx).Query().
		Where(role.IDEQ(roleID), role.DeletedAtIsNil()).
		QueryMenuGrants().Where(rolemenu.DeletedAtIsNil()).
		QueryMenu().Where(menu.DeletedAtIsNil()).
		Order(menu.ByParentID(), menu.ByOrderNum()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	links := make([]*accessrepo.RoleMenuLink, 0, len(pos))
	for _, po := range pos {
		links = append(links, &accessrepo.RoleMenuLink{MenuID: po.ID, ParentID: po.ParentID})
	}
	return links, nil
}

// ReplaceRoleMenus replaces the role's active Menu Grant and writes
// menu_check_strictly, returning the currently assigned System User
// identities. RoleUsecase validates and dedupes menuIDs; the caller owns the
// transaction and Redis rollback contract. Full replacement works on the
// active set only (ADR 0002): the current link rows are soft-deleted
// (stamping actorID), then the new generation is inserted.
func (r *roleRepo) ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool, actorID int64) ([]int64, error) {
	if len(menuIDs) > 0 {
		count, err := r.data.DB.Menu(ctx).Query().Where(menu.IDIn(menuIDs...), menu.DeletedAtIsNil()).Count(ctx)
		if err != nil {
			return nil, err
		}
		if count != len(sortedIDs(menuIDs)) {
			return nil, access.ErrRoleMenuNotFound
		}
	}
	userIDs, err := r.data.DB.Role(ctx).Query().Where(role.IDEQ(roleID), role.DeletedAtIsNil()).
		QueryUserLinks().Where(userrole.DeletedAtIsNil()).
		QueryUser().Where(user.DeletedAtIsNil()).IDs(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := r.data.DB.RoleMenu(ctx).Update().
		Where(rolemenu.RoleIDEQ(roleID), rolemenu.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Save(ctx); err != nil {
		return nil, err
	}
	if _, err := r.data.DB.Role(ctx).UpdateOneID(roleID).
		SetMenuCheckStrictly(menuCheckStrictly).
		SetUpdateBy(actorID).
		Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrRoleNotFound
		}
		return nil, err
	}
	if err := insertMenuGrants(ctx, r.data, roleID, menuIDs, actorID); err != nil {
		return nil, err
	}
	return userIDs, nil
}

// insertMenuGrants bulk-inserts one active sys_role_menu row per menu id,
// stamped with the actor (ADR 0002). No-op for an empty set.
func insertMenuGrants(ctx context.Context, data *store.Data, roleID int64, menuIDs []int64, actorID int64) error {
	if len(menuIDs) == 0 {
		return nil
	}
	builders := make([]*ent.RoleMenuCreate, 0, len(menuIDs))
	for _, menuID := range menuIDs {
		builders = append(builders, data.DB.RoleMenu(ctx).Create().
			SetID(newSnowflakeID()).
			SetRoleID(roleID).
			SetMenuID(menuID).
			SetCreateBy(actorID).
			SetUpdateBy(actorID))
	}
	return data.DB.RoleMenu(ctx).CreateBulk(builders...).Exec(ctx)
}
