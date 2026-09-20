package userinfra

import (
	"glex/internal/infra/store"

	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	userdomain "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	userrepo "glex/internal/domain/user/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/ent/userpost"
	"glex/internal/infra/ent/userrole"
)

// toBiz maps an ent User PO to a userentity.User DO. The credential column is
// deliberately not mapped: only authn's adapters read it (ADR 0005).
func toBiz(po *ent.User) *userentity.User {
	u := &userentity.User{
		ID:        po.ID,
		UserName:  po.UserName,
		NickName:  po.NickName,
		Gender:    int32(po.Gender),
		Status:    int32(po.Status),
		Avatar:    po.Avatar,
		Remark:    po.Remark,
		DeptID:    po.DeptID,
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

type userRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewUserRepo creates a new UserRepo instance.
func NewUserRepo(data *store.Data) userrepo.UserRepo {
	return &userRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.user.user")),
	}
}

// userStoreError is the log-and-map for unexpected user-store failures (ADR
// 0007): the raw error is logged once at the handling site with its operation
// and returned as the module's sentinel, so no raw ent error crosses the
// repository boundary. Expected business conditions (ent.IsNotFound and
// friends) map silently at the call sites; a canceled request is benign and
// returns the sentinel without logging.
func userStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return userdomain.ErrUserStoreUnavailable
	}
	logger.ErrorContext(ctx, "user store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return userdomain.ErrUserStoreUnavailable
}

func (r *userRepo) FindByID(ctx context.Context, id int64) (*userentity.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(id), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, userStoreError(ctx, r.logger, "user.find_by_id", err)
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByUserName(ctx context.Context, userName string) (*userentity.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.UserNameEQ(userName), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		if ent.IsNotSingular(err) {
			return nil, userdomain.ErrUserNameAmbiguous
		}
		return nil, userStoreError(ctx, r.logger, "user.find_by_user_name", err)
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*userentity.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.EmailEQ(email), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, userStoreError(ctx, r.logger, "user.find_by_email", err)
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByPhoneNumber(ctx context.Context, phone string) (*userentity.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.PhoneNumberEQ(phone), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, userStoreError(ctx, r.logger, "user.find_by_phone_number", err)
	}
	return toBiz(po), nil
}

func (r *userRepo) ListUsers(ctx context.Context, req userrepo.ListUsersRequest) ([]*userentity.User, error) {
	query := r.data.DB.User(ctx).Query().
		Where(
			user.DeletedAtIsNil(),
			ents.ApplyFilter(req.Filter),
		)
	if len(req.DeptIDs) > 0 {
		query = query.Where(user.DeptIDIn(req.DeptIDs...))
	}
	pos, err := query.
		Order(ents.ApplyOrderBy(req.OrderBy)).
		Offset(req.Offset).
		Limit(req.Limit).
		All(ctx)
	if err != nil {
		return nil, userStoreError(ctx, r.logger, "user.list", err)
	}
	users := make([]*userentity.User, 0, len(pos))
	for _, po := range pos {
		users = append(users, toBiz(po))
	}
	return users, nil
}

func (r *userRepo) CreateUser(ctx context.Context, u *userentity.User) (*userentity.User, error) {
	b := r.data.DB.User(ctx).Create().
		SetID(newSnowflakeID()).
		SetUserName(u.UserName).
		SetNickName(u.NickName).
		SetGender(int8(u.Gender)).
		SetStatus(int8(u.Status)).
		SetAvatar(u.Avatar).
		SetRemark(u.Remark).
		SetDeptID(u.DeptID).
		SetCreateBy(u.CreateBy).
		SetUpdateBy(u.UpdateBy)
	if u.Email != "" {
		b.SetEmail(u.Email)
	}
	if u.PhoneNumber != "" {
		b.SetPhoneNumber(u.PhoneNumber)
	}
	po, err := b.Save(ctx)
	if err != nil {
		return nil, userStoreError(ctx, r.logger, "user.create", err)
	}
	return toBiz(po), nil
}

// UpdateUser re-reads the record, applies the mutation, and returns the
// persisted record with its pre-image for UserUsecase revocation
// classification. The caller owns the surrounding transaction.
func (r *userRepo) UpdateUser(ctx context.Context, u *userentity.User) (*userentity.UserUpdate, error) {
	result := &userentity.UserUpdate{}
	current, err := r.data.DB.User(ctx).Query().Where(user.IDEQ(u.ID), user.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, userStoreError(ctx, r.logger, "user.update", err)
	}
	upd := r.data.DB.User(ctx).UpdateOneID(u.ID).
		SetUserName(u.UserName).
		SetNickName(u.NickName).
		SetGender(int8(u.Gender)).
		SetStatus(int8(u.Status)).
		SetAvatar(u.Avatar).
		SetRemark(u.Remark).
		SetDeptID(u.DeptID).
		SetUpdateBy(u.UpdateBy)
	if u.Email != "" {
		upd.SetEmail(u.Email)
	}
	if u.PhoneNumber != "" {
		upd.SetPhoneNumber(u.PhoneNumber)
	}
	po, err := upd.Save(ctx)
	if err != nil {
		return nil, userStoreError(ctx, r.logger, "user.update", err)
	}
	result.User = toBiz(po)
	result.Previous = toBiz(current)
	return result, nil
}

// DeleteUser soft-deletes the user and its Role Assignment (sys_user_role)
// and Post Assignment (sys_user_post) links — link tables owned by the
// access and org modules, soft-deleted here in the same write, the
// established sys_user_role precedent (ADR 0002). The caller owns the
// transaction and session-revocation ordering.
func (r *userRepo) DeleteUser(ctx context.Context, req userrepo.DeleteUserRequest) ([]int64, error) {
	if _, err := r.data.DB.UserRole(ctx).Update().
		Where(userrole.UserIDEQ(req.ID), userrole.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Save(ctx); err != nil {
		return nil, userStoreError(ctx, r.logger, "user.delete", err)
	}
	if _, err := r.data.DB.UserPost(ctx).Update().
		Where(userpost.UserIDEQ(req.ID), userpost.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Save(ctx); err != nil {
		return nil, userStoreError(ctx, r.logger, "user.delete", err)
	}
	err := r.data.DB.User(ctx).UpdateOneID(req.ID).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, userStoreError(ctx, r.logger, "user.delete", err)
	}
	return []int64{req.ID}, nil
}

// CountByDeptID counts active users whose department reference points at
// deptID — the read behind the org module's department guards (ADR 0013).
func (r *userRepo) CountByDeptID(ctx context.Context, deptID int64) (int, error) {
	n, err := r.data.DB.User(ctx).Query().
		Where(user.DeptIDEQ(deptID), user.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return 0, userStoreError(ctx, r.logger, "user.count_by_dept_id", err)
	}
	return n, nil
}
