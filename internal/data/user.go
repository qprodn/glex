package data

import (
	"context"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/user"
)

// toBiz maps an ent User PO to a biz.User DO.
func toBiz(po *ent.User) *biz.User {
	u := &biz.User{
		ID:        po.ID,
		UserName:  po.UserName,
		NickName:  po.NickName,
		Gender:    int32(po.Gender),
		Status:    int32(po.Status),
		Password:  po.Password,
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
	data     *Data
	security *securityChangeCoordinator
}

// NewUserRepo creates a new UserRepo instance.
func NewUserRepo(data *Data, security *securityChangeCoordinator) biz.UserRepo {
	return &userRepo{data: data, security: security}
}

func (r *userRepo) FindByID(ctx context.Context, id int64) (*biz.User, error) {
	po, err := r.data.db.User.Query().
		Where(user.IDEQ(id), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByUserName(ctx context.Context, userName string) (*biz.User, error) {
	po, err := r.data.db.User.Query().
		Where(user.UserNameEQ(userName), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*biz.User, error) {
	po, err := r.data.db.User.Query().
		Where(user.EmailEQ(email), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) FindByPhoneNumber(ctx context.Context, phone string) (*biz.User, error) {
	po, err := r.data.db.User.Query().
		Where(user.PhoneNumberEQ(phone), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrUserNotFound
		}
		return nil, err
	}
	return toBiz(po), nil
}

func (r *userRepo) ListUsers(ctx context.Context, opts ...biz.ListOption) ([]*biz.User, error) {
	o := biz.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.db.User.Query().
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
	users := make([]*biz.User, 0, len(pos))
	for _, po := range pos {
		users = append(users, toBiz(po))
	}
	return users, nil
}

func (r *userRepo) CreateUser(ctx context.Context, u *biz.User) (*biz.User, error) {
	b := r.data.db.User.Create().
		SetID(newSnowflakeID()).
		SetUserName(u.UserName).
		SetNickName(u.NickName).
		SetGender(int8(u.Gender)).
		SetStatus(int8(u.Status)).
		SetPassword(u.Password).
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

func (r *userRepo) UpdateUser(ctx context.Context, u *biz.User, passwordPolicy biz.SessionRevocation) (*biz.User, error) {
	var po *ent.User
	err := r.security.Run(ctx, func(change *securityChangeTx) error {
		if err := change.LockSystemUsers(ctx, u.ID); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		current, err := change.tx.User.Query().Where(user.IDEQ(u.ID), user.DeletedAtIsNil()).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		revocation := u.SessionRevocationFrom(toBiz(current), passwordPolicy)
		upd := change.tx.User.UpdateOneID(u.ID).
			SetUserName(u.UserName).
			SetNickName(u.NickName).
			SetGender(int8(u.Gender)).
			SetStatus(int8(u.Status)).
			SetAvatar(u.Avatar).
			SetRemark(u.Remark)
		if u.Password != "" {
			upd.SetPassword(u.Password)
		}
		if u.Email != "" {
			upd.SetEmail(u.Email)
		}
		if u.PhoneNumber != "" {
			upd.SetPhoneNumber(u.PhoneNumber)
		}
		po, err = upd.Save(ctx)
		if err != nil {
			return err
		}
		change.AffectSystemUser(u.ID, revocation)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return toBiz(po), nil
}

// DeleteUser soft-deletes the user and hard-deletes its Role Assignment
// (sys_user_role rows) in one transaction (RuoYi semantics: a deleted user
// loses its role links for good).
func (r *userRepo) DeleteUser(ctx context.Context, id int64) error {
	return r.security.Run(ctx, func(change *securityChangeTx) error {
		if err := change.LockRoleAssignment(ctx, id); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		roleIDs, err := change.tx.User.Query().Where(user.IDEQ(id), user.DeletedAtIsNil()).QueryRoles().IDs(ctx)
		if err != nil {
			return err
		}
		if err := change.LockRoles(ctx, roleIDs...); err != nil {
			return err
		}
		if err := change.LockSystemUsers(ctx, id); err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		if exists, err := change.tx.User.Query().Where(user.IDEQ(id), user.DeletedAtIsNil()).Exist(ctx); err != nil {
			return err
		} else if !exists {
			return biz.ErrUserNotFound
		}
		// The relationship seam above prevents another replacement while the
		// concrete Role/User locks are acquired. Re-read under those locks
		// before clearing the Role Assignment.
		if _, err := change.tx.User.Query().Where(user.IDEQ(id), user.DeletedAtIsNil()).QueryRoles().IDs(ctx); err != nil {
			return err
		}
		err = change.tx.User.UpdateOneID(id).
			SetDeletedAt(time.Now()).
			ClearRoles().
			Exec(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return biz.ErrUserNotFound
			}
			return err
		}
		change.AffectSystemUsers(id)
		return nil
	})
}
