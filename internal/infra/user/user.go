package userinfra

import (
	"glex/internal/infra/store"

	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	userdomain "glex/internal/domain/user"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"
)

// toBiz maps an ent User PO to a userdomain.User DO. The credential column is
// deliberately not mapped: only authn's adapters read it (ADR 0016).
func toBiz(po *ent.User) *userdomain.User {
	u := &userdomain.User{
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

type userRepo struct {
	data *store.Data
}

// NewUserRepo creates a new UserRepo instance.
func NewUserRepo(data *store.Data) userdomain.UserRepo {
	return &userRepo{data: data}
}

func (r *userRepo) FindByID(ctx context.Context, id int64) (*userdomain.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(id), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByUserName(ctx context.Context, userName string) (*userdomain.User, error) {
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
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*userdomain.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.EmailEQ(email), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByPhoneNumber(ctx context.Context, phone string) (*userdomain.User, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.PhoneNumberEQ(phone), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) ListUsers(ctx context.Context, opts ...userdomain.ListOption) ([]*userdomain.User, error) {
	o := userdomain.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.User(ctx).Query().
		Where(
			user.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]*userdomain.User, 0, len(pos))
	for _, po := range pos {
		users = append(users, toBiz(po))
	}
	return users, nil
}

func (r *userRepo) CreateUser(ctx context.Context, u *userdomain.User) (*userdomain.User, error) {
	b := r.data.DB.User(ctx).Create().
		SetID(newSnowflakeID()).
		SetUserName(u.UserName).
		SetNickName(u.NickName).
		SetGender(int8(u.Gender)).
		SetStatus(int8(u.Status)).
		SetAvatar(u.Avatar).
		SetRemark(u.Remark)
	if u.Email != "" {
		b.SetEmail(u.Email)
	}
	if u.PhoneNumber != "" {
		b.SetPhoneNumber(u.PhoneNumber)
	}
	po, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toBiz(po), nil
}

// UpdateUser re-reads the record, applies the mutation, and returns the
// persisted record with its pre-image for UserUsecase revocation
// classification. The caller owns the surrounding transaction.
func (r *userRepo) UpdateUser(ctx context.Context, u *userdomain.User) (*userdomain.UserUpdate, error) {
	result := &userdomain.UserUpdate{}
	current, err := r.data.DB.User(ctx).Query().Where(user.IDEQ(u.ID), user.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, err
	}
	upd := r.data.DB.User(ctx).UpdateOneID(u.ID).
		SetUserName(u.UserName).
		SetNickName(u.NickName).
		SetGender(int8(u.Gender)).
		SetStatus(int8(u.Status)).
		SetAvatar(u.Avatar).
		SetRemark(u.Remark)
	if u.Email != "" {
		upd.SetEmail(u.Email)
	}
	if u.PhoneNumber != "" {
		upd.SetPhoneNumber(u.PhoneNumber)
	}
	po, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	result.User = toBiz(po)
	result.Previous = toBiz(current)
	return result, nil
}

// DeleteUser soft-deletes the user and hard-deletes its Role Assignment
// (sys_user_role rows). A deleted user loses its role links for good. The
// caller owns the transaction and session-revocation ordering.
func (r *userRepo) DeleteUser(ctx context.Context, id int64) ([]int64, error) {
	err := r.data.DB.User(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		ClearRoles().
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, userdomain.ErrUserNotFound
		}
		return nil, err
	}
	return []int64{id}, nil
}
