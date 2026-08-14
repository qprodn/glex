package data

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/role"
)

// toBizRole maps an ent Role PO to a biz.Role DO.
func toBizRole(po *ent.Role) *biz.Role {
	return &biz.Role{
		ID:        po.ID,
		RoleName:  po.RoleName,
		RoleKey:   po.RoleKey,
		RoleSort:  po.RoleSort,
		Status:    int32(po.Status),
		Remark:    po.Remark,
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
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

func (r *roleRepo) CreateRole(ctx context.Context, ro *biz.Role) (*biz.Role, error) {
	po, err := r.data.db.Role.Create().
		SetID(newSnowflakeID()).
		SetRoleName(ro.RoleName).
		SetRoleKey(ro.RoleKey).
		SetRoleSort(ro.RoleSort).
		SetStatus(int8(ro.Status)).
		SetRemark(ro.Remark).
		Save(ctx)
	if err != nil {
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

func (r *roleRepo) DeleteRole(ctx context.Context, id int64) error {
	err := r.data.db.Role.UpdateOneID(id).
		SetDeletedAt(time.Now()).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return biz.ErrRoleNotFound
		}
		return err
	}
	return nil
}
