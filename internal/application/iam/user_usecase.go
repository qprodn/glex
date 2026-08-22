package iam

import (
	"context"
	"errors"
	"log/slog"

	iamdomain "glex/internal/domain/iam"
)

// UserUsecase holds the system-user business rules.
type UserUsecase struct {
	repo    iamdomain.UserRepo
	tx      iamdomain.Transaction
	revoker iamdomain.SessionRevoker
	logger  *slog.Logger
}

// NewUserUsecase creates a UserUsecase. tx wraps security writes together
// with their session revocation; revoker performs the revocation.
func NewUserUsecase(repo iamdomain.UserRepo, tx iamdomain.Transaction, revoker iamdomain.SessionRevoker, logger *slog.Logger) *UserUsecase {
	return &UserUsecase{
		repo:    repo,
		tx:      tx,
		revoker: revoker,
		logger:  logger.With(slog.String("component", "application.iam.user")),
	}
}

// CreateUser validates uniqueness, hashes the password, and persists a user.
// Uniqueness is a code check against active rows (repo-wide convention; the DB
// has no unique constraints) — racy under concurrent creates, accepted.
func (uc *UserUsecase) CreateUser(ctx context.Context, u *iamdomain.User) (*iamdomain.User, error) {
	if u.Password == "" {
		return nil, iamdomain.ErrPasswordRequired
	}
	if err := uc.assertUnique(ctx, u, 0); err != nil {
		return nil, err
	}
	hashed, err := hashPassword(u.Password)
	if err != nil {
		return nil, err
	}
	u.Password = hashed
	return uc.repo.CreateUser(ctx, u)
}

// UpdateUser applies a partial update. Empty password means "leave unchanged";
// a non-empty password is bcrypt-hashed before persisting. Uniqueness is
// checked in code, excluding the record being updated. The mutation and the
// session revocation it classifies commit together: a Redis failure rolls the
// PostgreSQL write back (ADR-0010 ordering).
func (uc *UserUsecase) UpdateUser(ctx context.Context, u *iamdomain.User) (*iamdomain.User, error) {
	if err := uc.assertUnique(ctx, u, u.ID); err != nil {
		return nil, err
	}
	if u.Password != "" {
		hashed, err := hashPassword(u.Password)
		if err != nil {
			return nil, err
		}
		u.Password = hashed
	}
	passwordPolicy := iamdomain.RevokeAllLoginSessions()
	if u.Password != "" {
		if principal, ok := iamdomain.PrincipalFromContext(ctx); ok && principal.SystemUserID() == u.ID {
			passwordPolicy = iamdomain.PreserveLoginSession(principal.SessionID())
		}
	}
	var updated *iamdomain.User
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := uc.repo.UpdateUser(ctx, u)
		if err != nil {
			return err
		}
		updated = result.User
		policy := result.User.SessionRevocationFrom(result.Previous, passwordPolicy)
		return revokeSystemUsers(ctx, uc.revoker, iamdomain.SystemUserSessionRevocation{SystemUserID: u.ID, Policy: policy})
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// GetUser returns the user with the given id.
func (uc *UserUsecase) GetUser(ctx context.Context, id int64) (*iamdomain.User, error) {
	return uc.repo.FindByID(ctx, id)
}

// GetCurrentUserInfo reads the System iamdomain.User record afresh while retaining Roles
// and Functional Permissions from the authenticated Authorization Snapshot.
func (uc *UserUsecase) GetCurrentUserInfo(ctx context.Context, principal *iamdomain.Principal) (*iamdomain.CurrentUserInfo, error) {
	if principal == nil {
		return nil, iamdomain.ErrCredentialMissing
	}
	u, err := uc.repo.FindByID(ctx, principal.SystemUserID())
	if err != nil {
		return nil, err
	}
	return &iamdomain.CurrentUserInfo{
		User:        u,
		RoleKeys:    principal.RoleKeys(),
		Permissions: principal.Permissions(),
	}, nil
}

// ListUsers lists users matching the given options.
func (uc *UserUsecase) ListUsers(ctx context.Context, opts ...iamdomain.ListOption) ([]*iamdomain.User, error) {
	return uc.repo.ListUsers(ctx, opts...)
}

// DeleteUser soft-deletes the user with the given id and revokes their Login
// Sessions in the same transaction: a Redis failure rolls the delete back.
func (uc *UserUsecase) DeleteUser(ctx context.Context, id int64) error {
	return uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.repo.DeleteUser(ctx, id)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.revoker, affected...)
	})
}

// assertUnique checks user_name (always) and email/phone_number (when set) are
// not already taken, ignoring the record with excludeID (0 = none). A
// not-found from the repo is the expected "available" case. Checks run against
// active rows only, so soft-deleted values are reusable.
func (uc *UserUsecase) assertUnique(ctx context.Context, u *iamdomain.User, excludeID int64) error {
	if existing, err := uc.repo.FindByUserName(ctx, u.UserName); err != nil {
		if !errors.Is(err, iamdomain.ErrUserNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return iamdomain.ErrUserNameConflict
	}
	if u.Email != "" {
		if existing, err := uc.repo.FindByEmail(ctx, u.Email); err != nil {
			if !errors.Is(err, iamdomain.ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return iamdomain.ErrEmailConflict
		}
	}
	if u.PhoneNumber != "" {
		if existing, err := uc.repo.FindByPhoneNumber(ctx, u.PhoneNumber); err != nil {
			if !errors.Is(err, iamdomain.ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return iamdomain.ErrPhoneConflict
		}
	}
	return nil
}
