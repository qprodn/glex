package iaminfra

import (
	"glex/internal/infra/store"

	"context"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/iam"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
)

// userRoleRepo implements iam.UserRoleRepo over the sys_user_role join table
// (Role Assignment, 角色分配). Pattern: the role↔menu repo (sys_role_menu) —
// ent M2M edge traversal for reads, delete-then-insert in one tx for writes.
type userRoleRepo struct {
	data *store.Data
}

// NewUserRoleRepo creates a new UserRoleRepo instance.
func NewUserRoleRepo(data *store.Data) iam.UserRoleRepo {
	return &userRoleRepo{data: data}
}

// ListRoleAssignments reads every active role (the dialog's "all roles") with
// whether the user is assigned it, ordered by (role_sort, role_id). The user
// edge is filtered to the target user, so a non-empty Edges.Users means
// assigned; user existence itself is checked by the usecase.
func (r *userRoleRepo) ListRoleAssignments(ctx context.Context, userID int64) ([]*iam.RoleAssignment, error) {
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
	assignments := make([]*iam.RoleAssignment, 0, len(pos))
	for _, po := range pos {
		assignments = append(assignments, &iam.RoleAssignment{
			Role:     toBizRole(po),
			Assigned: len(po.Edges.Users) > 0,
		})
	}
	return assignments, nil
}

// ReplaceUserRoles atomically replaces the user's Role Assignment (RuoYi's
// delete-then-insert) under the authorization locks, returning the System
// User identities whose sessions must be revoked ({userID}). roleIDs are
// stored verbatim; the usecase validates and dedupes them first. A security
// write — pair with SessionRevocation inside Transaction.InTx.
func (r *userRoleRepo) ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]int64, error) {
	var affected []int64
	err := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		if err := lockRoleAssignment(ctx, r.data.DB, userID); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrUserNotFound
			}
			return err
		}
		oldRoleIDs, err := r.data.DB.User(ctx).Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).QueryRoles().IDs(ctx)
		if err != nil {
			return err
		}
		if err := lockRoles(ctx, r.data.DB, appendIDs(oldRoleIDs, roleIDs)...); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrRoleNotFound
			}
			return err
		}
		if len(roleIDs) > 0 {
			count, err := r.data.DB.Role(ctx).Query().Where(role.IDIn(roleIDs...), role.DeletedAtIsNil()).Count(ctx)
			if err != nil {
				return err
			}
			if count != len(sortedIDs(roleIDs)) {
				return iam.ErrRoleNotFound
			}
		}
		if err := lockSystemUsers(ctx, r.data.DB, userID); err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrUserNotFound
			}
			return err
		}
		if exists, err := r.data.DB.User(ctx).Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return iam.ErrUserNotFound
		}
		if _, err := r.data.DB.User(ctx).Query().Where(user.IDEQ(userID), user.DeletedAtIsNil()).QueryRoles().IDs(ctx); err != nil {
			return err
		}
		_, err = r.data.DB.User(ctx).UpdateOneID(userID).
			ClearRoles().
			AddRoleIDs(roleIDs...).
			Save(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return iam.ErrUserNotFound
			}
			return err
		}
		affected = []int64{userID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return affected, nil
}

// ListRoleUsers lists the active users assigned the role, applying the given
// list options (filter/order/offset/limit). The role's existence is checked
// by the usecase.
func (r *userRoleRepo) ListRoleUsers(ctx context.Context, roleID int64, opts ...iam.ListOption) ([]*iam.User, error) {
	o := iam.ListOptions{Limit: 20}
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
	users := make([]*iam.User, 0, len(pos))
	for _, po := range pos {
		users = append(users, toBiz(po))
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
