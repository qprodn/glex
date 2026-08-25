package iam

import (
	"context"
	"errors"
	"log/slog"

	"glex/internal/crossdomain/authn"
	authnmodel "glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
	iamdomain "glex/internal/domain/iam"
)

// UserUsecase holds the system-user business rules.
type UserUsecase struct {
	repo        iamdomain.UserRepo
	credentials authn.CredentialWriter
	tx          Transaction
	revoker     authn.SessionRevoker
	logger      *slog.Logger
}

// NewUserUsecase creates a UserUsecase. credentials stores password requests
// through the authn crossdomain contract (the credential column is authn's,
// per ADR 0016); tx wraps security writes together with their session
// revocation; revoker performs the revocation through the authn crossdomain
// contract.
func NewUserUsecase(repo iamdomain.UserRepo, credentials authn.CredentialWriter, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *UserUsecase {
	return &UserUsecase{
		repo:        repo,
		credentials: credentials,
		tx:          tx,
		revoker:     revoker,
		logger:      logger.With(slog.String("component", "application.iam.user")),
	}
}

// CreateUser validates uniqueness and persists the user together with the
// initial credential, which authn hashes and stores on the same transaction.
// Uniqueness is a code check against active rows (repo-wide convention; the DB
// has no unique constraints) — racy under concurrent creates, accepted.
func (uc *UserUsecase) CreateUser(ctx context.Context, u *iamdomain.User) (*iamdomain.User, error) {
	if u.Password == "" {
		return nil, iamdomain.ErrPasswordRequired
	}
	if err := uc.assertUnique(ctx, u, 0); err != nil {
		return nil, err
	}
	var created *iamdomain.User
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = uc.repo.CreateUser(ctx, u)
		if err != nil {
			return err
		}
		return uc.writeCredential(ctx, created.ID, u.Password)
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateUser applies a partial update. Empty password means "leave unchanged";
// a non-empty password is stored through authn's credential capability on the
// same transaction. Uniqueness is checked in code, excluding the record being
// updated. The mutation and the session revocation it classifies currently
// execute in the same transaction.
func (uc *UserUsecase) UpdateUser(ctx context.Context, u *iamdomain.User) (*iamdomain.User, error) {
	if err := uc.assertUnique(ctx, u, u.ID); err != nil {
		return nil, err
	}
	passwordPolicy := iamdomain.RevokeAllLoginSessions()
	if u.Password != "" {
		if principal, ok := authndomain.PrincipalFromContext(ctx); ok && principal.SystemUserID() == u.ID {
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
		if u.Password != "" {
			if err := uc.writeCredential(ctx, u.ID, u.Password); err != nil {
				return err
			}
		}
		// The receiver is the requested change; the persisted record carries
		// no password (authn owns the stored hash), so the classification
		// reads the request's password field.
		policy := u.SessionRevocationFrom(result.Previous, passwordPolicy)
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

// GetCurrentUserInfo reads the System User record afresh while retaining Roles
// and Functional Permissions from the authenticated Authorization Snapshot.
func (uc *UserUsecase) GetCurrentUserInfo(ctx context.Context, principal *authndomain.Principal) (*iamdomain.CurrentUserInfo, error) {
	if principal == nil {
		return nil, authndomain.ErrCredentialMissing
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

// DeleteUser soft-deletes the user with the given id and currently revokes
// their Login Sessions in the same transaction.
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

// writeCredential delegates a password request to authn's credential
// capability. It runs inside the caller's transaction so the profile row and
// the credential commit or roll back together.
func (uc *UserUsecase) writeCredential(ctx context.Context, systemUserID int64, password string) error {
	return uc.credentials.WriteSystemUserCredential(ctx, &authnmodel.SystemUserCredential{
		SystemUserID: systemUserID,
		Password:     password,
	})
}
