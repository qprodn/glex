package authninfra

import (
	"glex/internal/infra/store"

	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"glex/internal/conf"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"

	"github.com/sa-tokens/sa-token-go/core"
	satokenmanager "github.com/sa-tokens/sa-token-go/core/manager"
)

const (
	authorizationSnapshotSystemUserIDKey = "glex.system_user_id"
	authorizationSnapshotUserNameKey     = "glex.user_name"
)

type authLoginRepo struct {
	data          *store.Data
	logger        *slog.Logger
	manager       *core.Manager
	sessions      authnrepo.LoginSessionRepo
	fixedLifetime time.Duration
}

func newAuthLoginAdapter(data *store.Data, manager *core.Manager, sessions authnrepo.LoginSessionRepo, c *conf.Auth) *authLoginRepo {
	return &authLoginRepo{
		data:          data,
		logger:        data.Logger.With(slog.String("component", "infra.authn.auth_login")),
		manager:       manager,
		sessions:      sessions,
		fixedLifetime: c.GetFixedLifetime().AsDuration(),
	}
}

// NewAuthLoginBackend constructs the database and credential capabilities
// consumed by the login Application Usecase.
func NewAuthLoginBackend(data *store.Data, manager *core.Manager, sessions authnrepo.LoginSessionRepo, c *conf.Auth) authnrepo.AuthLoginBackend {
	return newAuthLoginAdapter(data, manager, sessions, c)
}

// FindLoginAccount loads the authentication view of the account answering a
// login identifier. This adapter is the only reader of the credential column
// (ADR 0005).
func (r *authLoginRepo) FindLoginAccount(ctx context.Context, userName string) (*authnentity.LoginAccount, error) {
	po, err := r.data.DB.User(ctx).Query().
		Where(user.UserNameEQ(userName), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, authndomain.ErrLoginAccountNotFound
		}
		if ent.IsNotSingular(err) {
			return nil, authndomain.ErrLoginAccountAmbiguous
		}
		return nil, authDatabaseBackendError(ctx, r.logger, "authn.find_login_account", err)
	}
	return &authnentity.LoginAccount{
		ID:           po.ID,
		UserName:     po.UserName,
		PasswordHash: po.Password,
		Status:       int32(po.Status),
	}, nil
}

// Revalidate re-reads the account inside the caller's Application transaction.
func (r *authLoginRepo) Revalidate(ctx context.Context, req authnrepo.RevalidateRequest) (*authnentity.LoginAccount, error) {
	if req.SystemUserID <= 0 || req.UserName == "" || req.PasswordHash == "" {
		return nil, authndomain.ErrInvalidCredentials
	}
	current, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(req.SystemUserID), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, authndomain.ErrInvalidCredentials
		}
		return nil, authDatabaseBackendError(ctx, r.logger, "authn.reload_login_system_user", err)
	}
	if current.UserName != req.UserName || current.Password != req.PasswordHash {
		return nil, authndomain.ErrInvalidCredentials
	}
	if current.Status != 0 {
		return nil, authndomain.ErrSystemUserDisabled
	}
	return &authnentity.LoginAccount{
		ID:           current.ID,
		UserName:     current.UserName,
		PasswordHash: current.Password,
		Status:       int32(current.Status),
	}, nil
}

// RecordLogin persists the login IP and time on the System User row inside
// the caller's transaction.
func (r *authLoginRepo) RecordLogin(ctx context.Context, req authnrepo.RecordLoginRequest) error {
	if err := r.data.DB.User(ctx).UpdateOneID(req.SystemUserID).
		SetLoginIP(req.SourceIP).
		SetLoginDate(req.LoginTime).
		Exec(ctx); err != nil {
		return authDatabaseBackendError(ctx, r.logger, "authn.update_last_login", err)
	}
	return nil
}

// IssueCredential mints the bearer credential (the only Manager.Login call
// site), replaces the shared Account Session snapshot, and registers the
// authoritative Login Session. Pure Redis — deliberately the last step of the
// login orchestration so its failure rolls the PostgreSQL writes back. On
// partial failure it destroys what it wrote before returning the error.
func (r *authLoginRepo) IssueCredential(ctx context.Context, req authnrepo.IssueCredentialRequest) (*authnentity.LoginResult, error) {
	if req.Snapshot == nil || req.Session == nil || req.Session.SystemUserID <= 0 || req.Session.UserName == "" {
		return nil, authndomain.ErrInvalidCredentials
	}
	token, err := loginAndReplaceAuthorizationSnapshot(r.manager, req.Snapshot, req.Session.DeviceType, r.fixedLifetime)
	if err != nil {
		return nil, authndomain.ErrAuthBackendUnavailable
	}
	if _, err := r.sessions.Create(ctx, authnrepo.CreateSessionRequest{BearerToken: token, Session: req.Session}); err != nil {
		r.cleanupUnregisteredToken(ctx, token, req.Session.SystemUserID)
		return nil, err
	}
	return &authnentity.LoginResult{AccessToken: token, ExpiresIn: r.fixedLifetime}, nil
}

// DiscardCredential tears down a credential whose transaction failed to
// commit: destroys the token, its authoritative Login Session, and the
// Account Session when it was the last one. Best-effort and idempotent — a
// leftover credential expires on its own.
//
// The ignored errors below are safe to swallow silently (ADR 0007): every
// call is log-and-mapped, so a backend failure already logs Error at the
// adapter exit that owns it (authRedisBackendError, the auth storage observe
// hook) before surfacing here as the availability sentinel. What is left to
// swallow is that sentinel plus benign conditions — context cancellation,
// keys already gone (ErrAuthKeyNotFound), a Login Session revoked
// concurrently (ErrLoginSessionNotOwned) — and a Warn here would double-log
// each failure.
func (r *authLoginRepo) DiscardCredential(ctx context.Context, req authnrepo.DiscardCredentialRequest) error {
	if req.SystemUserID <= 0 || req.BearerToken == "" {
		return nil
	}
	if session, err := r.sessions.FindByToken(ctx, req.BearerToken); err == nil && session != nil {
		_ = r.sessions.Logout(ctx, session.ID)
	}
	_ = r.manager.LogoutByToken(req.BearerToken)
	if remaining, err := r.sessions.ListBySystemUser(ctx, authnrepo.ListBySystemUserRequest{SystemUserID: req.SystemUserID}); err == nil && len(remaining) == 0 {
		_ = r.manager.DeleteSession(strconv.FormatInt(req.SystemUserID, 10))
	}
	return nil
}

// loginAndReplaceAuthorizationSnapshot is deliberately the only project call
// site of Manager.Login. Manager.Login clears the Account Session; SetMulti
// immediately restores identity, Roles, and Functional Permissions in one
// storage write before the token can leave the data wrapper.
func loginAndReplaceAuthorizationSnapshot(manager *core.Manager, snapshot *authnentity.AuthorizationSnapshot, deviceType string, lifetime time.Duration) (string, error) {
	loginID := strconv.FormatInt(snapshot.SystemUserID, 10)
	token, err := manager.Login(loginID, deviceType)
	if err != nil {
		return "", err
	}
	session, err := manager.GetSession(loginID)
	if err != nil {
		// Rollback is best-effort: LogoutByToken is log-and-mapped inside the
		// storage adapter (backend failures log Error there), and the token
		// being destroyed never left the data wrapper, so its loss is benign.
		_ = manager.LogoutByToken(token)
		return "", err
	}
	err = session.SetMulti(map[string]any{
		authorizationSnapshotSystemUserIDKey: strconv.FormatInt(snapshot.SystemUserID, 10),
		authorizationSnapshotUserNameKey:     snapshot.UserName,
		// Copy into a non-nil slice even when empty: a nil slice serializes
		// to JSON null, which the snapshot decoder rejects — locking a
		// legitimately role-less System User out with spurious 503s.
		satokenmanager.SessionKeyRoles:       copyStringSet(snapshot.RoleKeys),
		satokenmanager.SessionKeyPermissions: copyStringSet(snapshot.Permissions),
	}, lifetime)
	if err != nil {
		// Same best-effort rollback: log-and-mapped inside the storage
		// adapter, so silence here double-logs nothing.
		_ = manager.LogoutByToken(token)
		return "", err
	}
	return token, nil
}

func authDatabaseBackendError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return authndomain.ErrAuthBackendUnavailable
	}
	logger.ErrorContext(ctx, "authentication database operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return authndomain.ErrAuthBackendUnavailable
}

// copyStringSet copies values into a slice that is never nil, so an empty set
// round-trips through the Account Session as [] rather than null.
func copyStringSet(values []string) []string {
	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}

// cleanupUnregisteredToken destroys a token whose Login Session registration
// failed. Like DiscardCredential's cleanup, every call is log-and-mapped at
// its adapter exit, so a backend failure is logged Error there before the
// sentinel surfaces here, and silence at this swallow site double-logs
// nothing (ADR 0007).
func (r *authLoginRepo) cleanupUnregisteredToken(ctx context.Context, token string, systemUserID int64) {
	_ = r.manager.LogoutByToken(token)
	remaining, err := r.sessions.ListBySystemUser(ctx, authnrepo.ListBySystemUserRequest{SystemUserID: systemUserID})
	if err == nil && len(remaining) == 0 {
		_ = r.manager.DeleteSession(strconv.FormatInt(systemUserID, 10))
	}
}
