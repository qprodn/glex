package authninfra

import (
	"glex/internal/infra/store"

	"context"
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
func (r *authLoginRepo) Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*authnentity.LoginAccount, error) {
	if systemUserID <= 0 || userName == "" || passwordHash == "" {
		return nil, authndomain.ErrInvalidCredentials
	}
	current, err := r.data.DB.User(ctx).Query().
		Where(user.IDEQ(systemUserID), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, authndomain.ErrInvalidCredentials
		}
		return nil, authDatabaseBackendError(ctx, r.logger, "reload_login_system_user", err)
	}
	if current.UserName != userName || current.Password != passwordHash {
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
func (r *authLoginRepo) RecordLogin(ctx context.Context, systemUserID int64, sourceIP string, loginTime time.Time) error {
	if err := r.data.DB.User(ctx).UpdateOneID(systemUserID).
		SetLoginIP(sourceIP).
		SetLoginDate(loginTime).
		Exec(ctx); err != nil {
		return authDatabaseBackendError(ctx, r.logger, "update_last_login", err)
	}
	return nil
}

// IssueCredential mints the bearer credential (the only Manager.Login call
// site), replaces the shared Account Session snapshot, and registers the
// authoritative Login Session. Pure Redis — deliberately the last step of the
// login orchestration so its failure rolls the PostgreSQL writes back. On
// partial failure it destroys what it wrote before returning the error.
func (r *authLoginRepo) IssueCredential(ctx context.Context, snapshot *authnentity.AuthorizationSnapshot, session *authnentity.LoginSession) (*authnentity.LoginResult, error) {
	if snapshot == nil || session == nil || session.SystemUserID <= 0 || session.UserName == "" {
		return nil, authndomain.ErrInvalidCredentials
	}
	token, err := loginAndReplaceAuthorizationSnapshot(r.manager, snapshot, session.DeviceType, r.fixedLifetime)
	if err != nil {
		return nil, authndomain.ErrAuthBackendUnavailable
	}
	if _, err := r.sessions.Create(ctx, token, session); err != nil {
		r.cleanupUnregisteredToken(ctx, token, session.SystemUserID)
		return nil, err
	}
	return &authnentity.LoginResult{AccessToken: token, ExpiresIn: r.fixedLifetime}, nil
}

// DiscardCredential tears down a credential whose transaction failed to
// commit: destroys the token, its authoritative Login Session, and the
// Account Session when it was the last one. Best-effort and idempotent — a
// leftover credential expires on its own.
func (r *authLoginRepo) DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error {
	if systemUserID <= 0 || bearerToken == "" {
		return nil
	}
	if session, err := r.sessions.FindByToken(ctx, bearerToken); err == nil && session != nil {
		_ = r.sessions.Logout(ctx, session.ID)
	}
	_ = r.manager.LogoutByToken(bearerToken)
	if remaining, err := r.sessions.ListBySystemUser(ctx, systemUserID); err == nil && len(remaining) == 0 {
		_ = r.manager.DeleteSession(strconv.FormatInt(systemUserID, 10))
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
		_ = manager.LogoutByToken(token)
		return "", err
	}
	return token, nil
}

func authDatabaseBackendError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	logger.WarnContext(ctx, "authentication database operation failed",
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

func (r *authLoginRepo) cleanupUnregisteredToken(ctx context.Context, token string, systemUserID int64) {
	_ = r.manager.LogoutByToken(token)
	remaining, err := r.sessions.ListBySystemUser(ctx, systemUserID)
	if err == nil && len(remaining) == 0 {
		_ = r.manager.DeleteSession(strconv.FormatInt(systemUserID, 10))
	}
}
