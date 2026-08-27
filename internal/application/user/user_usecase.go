package user

import (
	"context"
	"log/slog"

	"glex/internal/crossdomain/authn"
	authnmodel "glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	userdomain "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	userrepo "glex/internal/domain/user/repository"
	userservice "glex/internal/domain/user/service"
)

// UserUsecase holds the system-user business rules.
type UserUsecase struct {
	repo        userrepo.UserRepo
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
func NewUserUsecase(repo userrepo.UserRepo, credentials authn.CredentialWriter, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *UserUsecase {
	return &UserUsecase{
		repo:        repo,
		credentials: credentials,
		tx:          tx,
		revoker:     revoker,
		logger:      logger.With(slog.String("component", "application.user")),
	}
}

// CreateUser validates uniqueness and persists the user together with the
// initial credential, which authn hashes and stores on the same transaction.
// Uniqueness is a code check against active rows (repo-wide convention; the DB
// has no unique constraints) — racy under concurrent creates, accepted.
func (uc *UserUsecase) CreateUser(ctx context.Context, u *userentity.User) (*userentity.User, error) {
	if u.Password == "" {
		return nil, userdomain.ErrPasswordRequired
	}
	if err := userservice.AssertUnique(ctx, uc.repo, u, 0); err != nil {
		return nil, err
	}
	var created *userentity.User
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
func (uc *UserUsecase) UpdateUser(ctx context.Context, u *userentity.User) (*userentity.User, error) {
	if err := userservice.AssertUnique(ctx, uc.repo, u, u.ID); err != nil {
		return nil, err
	}
	passwordPolicy := userentity.RevokeAllLoginSessions()
	if u.Password != "" {
		if principal, ok := authnservice.PrincipalFromContext(ctx); ok && principal.SystemUserID() == u.ID {
			passwordPolicy = userentity.PreserveLoginSession(principal.SessionID())
		}
	}
	var updated *userentity.User
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
		return revokeSystemUsers(ctx, uc.revoker, userentity.SystemUserSessionRevocation{SystemUserID: u.ID, Policy: policy})
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// GetUser returns the user with the given id.
func (uc *UserUsecase) GetUser(ctx context.Context, id int64) (*userentity.User, error) {
	return uc.repo.FindByID(ctx, id)
}

// GetCurrentUserInfo reads the System User record afresh while retaining Roles
// and Functional Permissions from the authenticated Authorization Snapshot.
func (uc *UserUsecase) GetCurrentUserInfo(ctx context.Context, principal *authnentity.Principal) (*userentity.CurrentUserInfo, error) {
	if principal == nil {
		return nil, authndomain.ErrCredentialMissing
	}
	u, err := uc.repo.FindByID(ctx, principal.SystemUserID())
	if err != nil {
		return nil, err
	}
	return &userentity.CurrentUserInfo{
		User:        u,
		RoleKeys:    principal.RoleKeys(),
		Permissions: principal.Permissions(),
	}, nil
}

// ListUsers lists users matching the given options.
func (uc *UserUsecase) ListUsers(ctx context.Context, opts ...userrepo.ListOption) ([]*userentity.User, error) {
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

// writeCredential delegates a password request to authn's credential
// capability. It runs inside the caller's transaction so the profile row and
// the credential commit or roll back together.
func (uc *UserUsecase) writeCredential(ctx context.Context, systemUserID int64, password string) error {
	return uc.credentials.WriteSystemUserCredential(ctx, &authnmodel.SystemUserCredential{
		SystemUserID: systemUserID,
		Password:     password,
	})
}
