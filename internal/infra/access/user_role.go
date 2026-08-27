package accessinfra

import (
	"glex/internal/infra/store"

	"context"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/access"
	accessrepo "glex/internal/domain/access/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
)

// userRoleRepo implements accessrepo.UserRoleRepo over the sys_user_role join
// table (Role Assignment, 角色分配). Pattern: the role↔menu repo (sys_role_menu) —
// ent M2M edge traversal for reads, delete-then-insert in one tx for writes.
// The user rows it reaches through the edge are access's storage reality until
// the tables split (ADR 0016); they surface only as RoleUser snapshots.
type userRoleRepo struct {
	data *store.Data
}

// NewUserRoleRepo creates a new UserRoleRepo instance.
func NewUserRoleRepo(data *store.Data) accessrepo.UserRoleRepo {
	return &userRoleRepo{data: data}
}

// toRoleUser maps an ent User PO to the by-role endpoint's RoleUser snapshot.
// The credential column is deliberately not mapped: it is authn's (ADR 0016).
func toRoleUser(po *ent.User) *accessrepo.RoleUser {
	u := &accessrepo.RoleUser{
		ID:        po.ID,
		UserName:  po.UserName,
		NickName:  po.NickName,
		Gender:    int32(po.Gender),
		Status:    int32(po.Status),
		Avatar:    po.Avatar,
		Remark:    po.Remark,
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		LoginIP:   po.LoginIP,
		LoginDate: po.LoginDate,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
	}
	if po.Email != nil {
		u.Email = *po.Email
	}
	if po.PhoneNumber != nil {
		u.PhoneNumber = *po.PhoneNumber
	}
	return u
}

// UserExists reports whether the System User exists and is not soft-deleted —
// the storage behind the by-user endpoints' existence gate.
func (r *userRoleRepo) UserExists(ctx context.Context, userID int64) (bool, error) {
	return r.data.DB.User(ctx).Query().
		Where(user.IDEQ(userID), user.DeletedAtIsNil()).
		Exist(ctx)
}

// ListRoleAssignments reads every active role (the dialog's "all roles") with
// whether the user is assigned it, ordered by (role_sort, role_id). The user
// edge is filtered to the target user, so a non-empty Edges.Users means
// assigned; user existence itself is checked by RoleAssignmentUsecase.
func (r *userRoleRepo) ListRoleAssignments(ctx context.Context, userID int64) ([]*accessrepo.RoleAssignment, error) {
	pos, err := r.data.DB.Role(ctx).Query().
		Where(role.DeletedAtIsNil()).
		Order(role.ByRoleSort(), role.ByID()).
		WithUsers(func(q *ent.UserQuery) {
			q.Where(user.IDEQ(userID))
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}
	assignments := make([]*accessrepo.RoleAssignment, 0, len(pos))
	for _, po := range pos {
		assignments = append(assignments, &accessrepo.RoleAssignment{
			Role:     toBizRole(po),
			Assigned: len(po.Edges.Users) > 0,
		})
	}
	return assignments, nil
}

// ReplaceUserRoles replaces the user's Role Assignment, returning the System
// User identities whose sessions must be revoked ({userID}). RoleAssignmentUsecase
// validates and dedupes roleIDs and owns the transaction and Redis rollback
// contract.
func (r *userRoleRepo) ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) > 0 {
		count, err := r.data.DB.Role(ctx).Query().Where(role.IDIn(roleIDs...), role.DeletedAtIsNil()).Count(ctx)
		if err != nil {
			return nil, err
		}
		if count != len(sortedIDs(roleIDs)) {
			return nil, access.ErrRoleNotFound
		}
	}
	_, err := r.data.DB.User(ctx).UpdateOneID(userID).
		ClearRoles().
		AddRoleIDs(roleIDs...).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, access.ErrUserNotFound
		}
		return nil, err
	}
	return []int64{userID}, nil
}

// ListRoleUsers lists the active users assigned the role, applying the given
// list options (filter/order/offset/limit). The role's existence is checked
// by RoleAssignmentUsecase.
func (r *userRoleRepo) ListRoleUsers(ctx context.Context, roleID int64, opts ...accessrepo.ListOption) ([]*accessrepo.RoleUser, error) {
	o := accessrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.User(ctx).Query().
		Where(
			user.DeletedAtIsNil(),
			user.HasRolesWith(role.IDEQ(roleID)),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]*accessrepo.RoleUser, 0, len(pos))
	for _, po := range pos {
		users = append(users, toRoleUser(po))
	}
	return users, nil
}

// CountUserRoles counts the users assigned the role — the guard behind "a
// role with users cannot be deleted or disabled".
func (r *userRoleRepo) CountUserRoles(ctx context.Context, roleID int64) (int, error) {
	return r.data.DB.Role(ctx).Query().
		Where(role.IDEQ(roleID), role.DeletedAtIsNil()).
		QueryUsers().
		Where(user.DeletedAtIsNil()).
		Count(ctx)
}
