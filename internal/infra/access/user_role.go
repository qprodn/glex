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
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/ent/userrole"
)

// userRoleRepo implements accessrepo.UserRoleRepo over the sys_user_role link
// rows (Role Assignment, 角色分配). Pattern: the role↔menu repo (sys_role_menu)
// — link-entity traversal for reads, soft-delete-then-insert in one tx for
// writes (ADR 0002: every table carries the audit fields; no row is
// hard-deleted). The user rows it reaches through the links are access's
// storage reality until the tables split (ADR 0005); they surface only as
// RoleUser snapshots.
type userRoleRepo struct {
	data *store.Data
}

// NewUserRoleRepo creates a new UserRoleRepo instance.
func NewUserRoleRepo(data *store.Data) accessrepo.UserRoleRepo {
	return &userRoleRepo{data: data}
}

// toRoleUser maps an ent User PO to the by-role endpoint's RoleUser snapshot.
// The credential column is deliberately not mapped: it is authn's (ADR 0005).
func toRoleUser(po *ent.User) *accessentity.RoleUser {
	u := &accessentity.RoleUser{
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
// whether the user is assigned it, ordered by (role_sort, id). The
// assigned flag comes from the user's active link rows; user existence itself
// is checked by RoleAssignmentUsecase.
func (r *userRoleRepo) ListRoleAssignments(ctx context.Context, userID int64) ([]*accessentity.RoleAssignment, error) {
	assignedIDs, err := r.data.DB.UserRole(ctx).Query().
		Where(userrole.UserIDEQ(userID), userrole.DeletedAtIsNil()).
		QueryRole().
		Where(role.DeletedAtIsNil()).
		IDs(ctx)
	if err != nil {
		return nil, err
	}
	assigned := make(map[int64]struct{}, len(assignedIDs))
	for _, id := range assignedIDs {
		assigned[id] = struct{}{}
	}
	pos, err := r.data.DB.Role(ctx).Query().
		Where(role.DeletedAtIsNil()).
		Order(role.ByRoleSort(), role.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	assignments := make([]*accessentity.RoleAssignment, 0, len(pos))
	for _, po := range pos {
		_, ok := assigned[po.ID]
		assignments = append(assignments, &accessentity.RoleAssignment{
			Role:     toBizRole(po),
			Assigned: ok,
		})
	}
	return assignments, nil
}

// ReplaceUserRoles replaces the user's Role Assignment, returning the System
// User identities whose sessions must be revoked ({userID}). RoleAssignmentUsecase
// validates and dedupes roleIDs and owns the transaction and Redis rollback
// contract. Full replacement works on the active set only (ADR 0002): the
// current rows are soft-deleted (stamping actorID), then the new generation
// is inserted with fresh snowflake ids.
func (r *userRoleRepo) ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64, actorID int64) ([]int64, error) {
	if len(roleIDs) > 0 {
		count, err := r.data.DB.Role(ctx).Query().Where(role.IDIn(roleIDs...), role.DeletedAtIsNil()).Count(ctx)
		if err != nil {
			return nil, err
		}
		if count != len(sortedIDs(roleIDs)) {
			return nil, access.ErrRoleNotFound
		}
	}
	exists, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(userID), user.DeletedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, access.ErrUserNotFound
	}
	if _, err := r.data.DB.UserRole(ctx).Update().
		Where(userrole.UserIDEQ(userID), userrole.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Save(ctx); err != nil {
		return nil, err
	}
	if len(roleIDs) > 0 {
		builders := make([]*ent.UserRoleCreate, 0, len(roleIDs))
		for _, roleID := range roleIDs {
			builders = append(builders, r.data.DB.UserRole(ctx).Create().
				SetID(newSnowflakeID()).
				SetUserID(userID).
				SetRoleID(roleID).
				SetCreateBy(actorID).
				SetUpdateBy(actorID))
		}
		if err := r.data.DB.UserRole(ctx).CreateBulk(builders...).Exec(ctx); err != nil {
			return nil, err
		}
	}
	return []int64{userID}, nil
}

// ListRoleUsers lists the active users assigned the role, applying the given
// list options (filter/order/offset/limit). The role's existence is checked
// by RoleAssignmentUsecase.
func (r *userRoleRepo) ListRoleUsers(ctx context.Context, roleID int64, opts ...accessrepo.ListOption) ([]*accessentity.RoleUser, error) {
	o := accessrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.User(ctx).Query().
		Where(
			user.DeletedAtIsNil(),
			user.HasRoleAssignmentsWith(userrole.RoleIDEQ(roleID), userrole.DeletedAtIsNil()),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]*accessentity.RoleUser, 0, len(pos))
	for _, po := range pos {
		users = append(users, toRoleUser(po))
	}
	return users, nil
}

// CountUserRoles counts the users assigned the role — the guard behind "a
// role with users cannot be deleted or disabled". Only active links count
// (ADR 0002).
func (r *userRoleRepo) CountUserRoles(ctx context.Context, roleID int64) (int, error) {
	return r.data.DB.UserRole(ctx).Query().
		Where(userrole.RoleIDEQ(roleID), userrole.DeletedAtIsNil()).
		QueryUser().
		Where(user.DeletedAtIsNil()).
		Count(ctx)
}
