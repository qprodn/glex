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
	data     *Data
	security *securityChangeCoordinator
}

// NewUserRoleRepo creates a new UserRoleRepo instance.
func NewUserRoleRepo(data *Data, security *securityChangeCoordinator) biz.UserRoleRepo {
	return &userRoleRepo{data: data, security: security}
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
	return r.security.Run(ctx, func(change *securityChangeTx) error {
		if err := change.LockRoleAssignment(ctx, userID); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		oldRoleIDs, err := change.tx.User.Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).QueryRoles().IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockRoles(ctx, appendIDs(oldRoleIDs, roleIDs)...); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrRoleNotFound
			}
			return err
		}
		if len(roleIDs) > 0 {
			count, err := change.tx.Role.Query().Where(role.IDIn(roleIDs...), role.DeletedAtIsNil()).Count(ctx)
			if err != nil {
				return err
			}
			if count != len(sortedIDs(roleIDs)) {
				return biz.ErrRoleNotFound
			}
		}
		if err := change.LockSystemUsers(ctx, userID); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		if exists, err := change.tx.User.Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return biz.ErrUserNotFound
		}
		if _, err := change.tx.User.Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).QueryRoles().IDs(ctx); err != nil {
			return err
		}
		_, err = change.tx.User.UpdateOneID(userID).
			ClearRoles().
			AddRoleIDs(roleIDs...).
			Save(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		change.AffectSystemUsers(userID)
		return nil
	})
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
