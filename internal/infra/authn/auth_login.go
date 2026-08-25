package authninfra

import (
	"glex/internal/infra/store"

	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"glex/internal/conf"
	authndomain "glex/internal/domain/authn"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"

	"github.com/sa-tokens/sa-token-go/core"
	satokenmanager "github.com/sa-tokens/sa-token-go/core/manager"
)

const (
	authorizationSnapshotSystemUserIDKey = "glex.system_user_id"
	authorizationSnapshotUserNameKey     = "glex.user_name"
	superadminRoleKey                    = "superadmin"
	superadminPermission                 = "*:*:*"
)

type authLoginRepo struct {
	data          *store.Data
	logger        *slog.Logger
	manager       *core.Manager
	sessions      authndomain.LoginSessionRepo
	fixedLifetime time.Duration
}

func newAuthLoginAdapter(data *store.Data, manager *core.Manager, sessions authndomain.LoginSessionRepo, c *conf.Auth) *authLoginRepo {
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
func NewAuthLoginBackend(data *store.Data, manager *core.Manager, sessions authndomain.LoginSessionRepo, c *conf.Auth) authndomain.AuthLoginBackend {
	return newAuthLoginAdapter(data, manager, sessions, c)
}

// FindLoginAccount loads the authentication view of the account answering a
// login identifier. This adapter is the only reader of the credential column
// (ADR 0016).
func (r *authLoginRepo) FindLoginAccount(ctx context.Context, userName string) (*authndomain.LoginAccount, error) {
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
		return nil, err
	}
	return &authndomain.LoginAccount{
		ID:           po.ID,
		UserName:     po.UserName,
		PasswordHash: po.Password,
		Status:       int32(po.Status),
	}, nil
}

// Revalidate re-reads the account inside the caller's Application transaction.
func (r *authLoginRepo) Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*authndomain.LoginAccount, error) {
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
	return &authndomain.LoginAccount{
		ID:           current.ID,
		UserName:     current.UserName,
		PasswordHash: current.Password,
		Status:       int32(current.Status),
	}, nil
}

// LoadSnapshot freshly reads the shared Authorization Snapshot inside the
// caller's transaction.
func (r *authLoginRepo) LoadSnapshot(ctx context.Context, systemUserID int64, userName string) (*authndomain.AuthorizationSnapshot, error) {
	snapshot, err := loadAuthorizationSnapshot(ctx, r.data.DB, systemUserID, userName)
	if err != nil {
		return nil, authDatabaseBackendError(ctx, r.logger, "load_authorization_snapshot", err)
	}
	return snapshot, nil
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
func (r *authLoginRepo) IssueCredential(ctx context.Context, snapshot *authndomain.AuthorizationSnapshot, session *authndomain.LoginSession) (*authndomain.LoginResult, error) {
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
	return &authndomain.LoginResult{AccessToken: token, ExpiresIn: r.fixedLifetime}, nil
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

func loadAuthorizationSnapshot(ctx context.Context, db *ent.Database, systemUserID int64, userName string) (*authndomain.AuthorizationSnapshot, error) {
	roles, err := db.Role(ctx).Query().
		Where(
			role.DeletedAtIsNil(),
			role.StatusEQ(0),
			role.HasUsersWith(user.IDEQ(systemUserID), user.DeletedAtIsNil()),
		).
		Order(role.ByRoleKey(), role.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	roleIDs := make([]int64, 0, len(roles))
	for _, assignedRole := range roles {
		roleIDs = append(roleIDs, assignedRole.ID)
	}
	menus := make([]*ent.Menu, 0)
	if len(roleIDs) > 0 {
		menus, err = db.Menu(ctx).Query().
			Where(
				menu.DeletedAtIsNil(),
				menu.StatusEQ(0),
				menu.HasRolesWith(role.IDIn(roleIDs...)),
			).
			Order(menu.ByPerms(), menu.ByID()).
			All(ctx)
		if err != nil {
			return nil, err
		}
	}
	return toBizAuthorizationSnapshot(systemUserID, userName, roles, menus), nil
}

func toBizAuthorizationSnapshot(systemUserID int64, userName string, roles []*ent.Role, menus []*ent.Menu) *authndomain.AuthorizationSnapshot {
	roleKeys := make([]string, 0, len(roles))
	superadmin := false
	for _, assignedRole := range roles {
		key := assignedRole.RoleKey
		if strings.TrimSpace(key) == "" {
			continue
		}
		roleKeys = append(roleKeys, key)
		if key == superadminRoleKey {
			superadmin = true
		}
	}
	permissions := make([]string, 0, len(menus)+1)
	seen := make(map[string]struct{}, len(menus)+1)
	for _, grantedMenu := range menus {
		permission := strings.TrimSpace(grantedMenu.Perms)
		if permission == "" || permission == superadminPermission && !superadmin {
			continue
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		permissions = append(permissions, permission)
	}
	if superadmin {
		if _, ok := seen[superadminPermission]; !ok {
			permissions = append(permissions, superadminPermission)
		}
	}
	sort.Strings(roleKeys)
	sort.Strings(permissions)
	return &authndomain.AuthorizationSnapshot{
		SystemUserID: systemUserID,
		UserName:     userName,
		RoleKeys:     roleKeys,
		Permissions:  permissions,
	}
}

// loginAndReplaceAuthorizationSnapshot is deliberately the only project call
// site of Manager.Login. Manager.Login clears the Account Session; SetMulti
// immediately restores identity, Roles, and Functional Permissions in one
// storage write before the token can leave the data wrapper.
func loginAndReplaceAuthorizationSnapshot(manager *core.Manager, snapshot *authndomain.AuthorizationSnapshot, deviceType string, lifetime time.Duration) (string, error) {
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
