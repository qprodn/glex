package data

import (
	"context"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/role"
	"glex/internal/data/ent/user"
)

// userRoleRepo implements biz.UserRoleRepo over the sys_user_role join table
// (Role Assignment, 角色分配). Pattern: the role↔menu repo (sys_role_menu) —
// ent M2M edge traversal for reads, delete-then-insert in one tx for writes.
type userRoleRepo struct {
	data *Data
}

// NewUserRoleRepo creates a new UserRoleRepo instance.
func NewUserRoleRepo(data *Data) biz.UserRoleRepo {
	return &userRoleRepo{data: data}
}

// ListRoleAssignments reads every active role (the dialog's "all roles") with
// whether the user is assigned it, ordered by (role_sort, role_id). The user
// edge is filtered to the target user, so a non-empty Edges.Users means
// assigned; user existence itself is checked by the usecase.
func (r *userRoleRepo) ListRoleAssignments(ctx context.Context, userID int64) ([]*biz.RoleAssignment, error) {
	pos, err := r.data.db.Role.Query().
		Where(role.DeletedAtIsNil()).
		Order(role.ByRoleSort(), role.ByID()).
		WithUsers(func(q *ent.UserQuery) {
			q.Where(user.IDEQ(userID))
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}
	assignments := make([]*biz.RoleAssignment, 0, len(pos))
	for _, po := range pos {
		assignments = append(assignments, &biz.RoleAssignment{
			Role:     toBizRole(po),
			Assigned: len(po.Edges.Users) > 0,
		})
	}
	return assignments, nil
}

// ReplaceUserRoles atomically replaces the user's Role Assignment (RuoYi's
// delete-then-insert) in one transaction. roleIDs are stored verbatim; the
// usecase validates and dedupes them first.
func (r *userRoleRepo) ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	tx, err := r.data.db.Tx(ctx)
	if err != nil {
		return err
	}
	_, err = tx.User.UpdateOneID(userID).
		ClearRoles().
		AddRoleIDs(roleIDs...).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return biz.ErrUserNotFound
		}
		return err
	}
	return tx.Commit()
}

// ListRoleUsers lists the active users assigned the role, applying the given
// list options (filter/order/offset/limit). The role's existence is checked
// by the usecase.
func (r *userRoleRepo) ListRoleUsers(ctx context.Context, roleID int64, opts ...biz.ListOption) ([]*biz.User, error) {
	o := biz.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.db.User.Query().
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
	users := make([]*biz.User, 0, len(pos))
	for _, po := range pos {
		users = append(users, toBiz(po))
	}
	return users, nil
}

// CountUserRoles counts the users assigned the role — the guard behind "a
// role with users cannot be deleted or disabled".
func (r *userRoleRepo) CountUserRoles(ctx context.Context, roleID int64) (int, error) {
	return r.data.db.Role.Query().
		Where(role.IDEQ(roleID), role.DeletedAtIsNil()).
		QueryUsers().
		Where(user.DeletedAtIsNil()).
		Count(ctx)
}
