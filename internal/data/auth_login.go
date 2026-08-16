package data

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"glex/internal/biz"
	"glex/internal/conf"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
	"glex/internal/data/ent/role"
	"glex/internal/data/ent/user"

	entsql "entgo.io/ent/dialect/sql"
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
	data          *Data
	manager       *core.Manager
	sessions      biz.LoginSessionRepo
	fixedLifetime time.Duration
}

// NewAuthLoginRepo constructs the sole data wrapper permitted to issue bearer
// credentials through Manager.Login.
func NewAuthLoginRepo(data *Data, manager *core.Manager, sessions biz.LoginSessionRepo, c *conf.Auth) biz.AuthLoginRepo {
	return &authLoginRepo{data: data, manager: manager, sessions: sessions, fixedLifetime: c.GetFixedLifetime().AsDuration()}
}

func (r *authLoginRepo) Issue(ctx context.Context, issue *biz.LoginIssue) (*biz.LoginResult, error) {
	if issue == nil || issue.SystemUserID <= 0 || issue.UserName == "" || issue.PasswordHash == "" {
		return nil, biz.ErrInvalidCredentials
	}
	tx, err := r.data.db.Tx(ctx)
	if err != nil {
		return nil, authDatabaseBackendError("begin_login_transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Global authorization-lock order: ascending System User ID. Login takes
	// exactly one identity lock. Security-changing mutations (issue 08) must
	// acquire every affected identity in that same order before Redis writes.
	// This is a separate statement from the authoritative re-read below: under
	// READ COMMITTED, the second statement receives a snapshot taken only after
	// any previous lock holder commits.
	_, err = tx.User.Query().
		Where(user.IDEQ(issue.SystemUserID)).
		Select(user.FieldID).
		Order(systemUserAuthorizationLock(issue.SystemUserID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrInvalidCredentials
		}
		return nil, authDatabaseBackendError("acquire_system_user_authorization_lock", err)
	}
	current, err := tx.User.Query().
		Where(user.IDEQ(issue.SystemUserID), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, biz.ErrInvalidCredentials
		}
		return nil, authDatabaseBackendError("reload_login_system_user", err)
	}
	if current.UserName != issue.UserName || current.Password != issue.PasswordHash {
		return nil, biz.ErrInvalidCredentials
	}
	if current.Status != 0 {
		return nil, biz.ErrSystemUserDisabled
	}

	snapshot, err := loadAuthorizationSnapshot(ctx, tx, current.ID, current.UserName)
	if err != nil {
		return nil, authDatabaseBackendError("load_authorization_snapshot", err)
	}
	token, err := loginAndReplaceAuthorizationSnapshot(r.manager, snapshot, issue.DeviceType, r.fixedLifetime)
	if err != nil {
		return nil, biz.ErrAuthBackendUnavailable
	}
	created, err := r.sessions.Create(ctx, token, &biz.LoginSession{
		SystemUserID: issue.SystemUserID,
		UserName:     issue.UserName,
		DeviceType:   issue.DeviceType,
		IPAddress:    issue.SourceIP,
		Browser:      issue.Browser,
		OS:           issue.OS,
		LoginTime:    issue.LoginTime,
	})
	if err != nil {
		r.cleanupUnregisteredToken(ctx, token, issue.SystemUserID)
		return nil, err
	}
	if err := tx.User.UpdateOneID(issue.SystemUserID).
		SetLoginIP(issue.SourceIP).
		SetLoginDate(issue.LoginTime).
		Exec(ctx); err != nil {
		_ = r.sessions.Logout(ctx, created.ID)
		return nil, authDatabaseBackendError("update_last_login", err)
	}
	if err := tx.Commit(); err != nil {
		_ = r.sessions.Logout(ctx, created.ID)
		return nil, authDatabaseBackendError("commit_login_transaction", err)
	}
	committed = true
	return &biz.LoginResult{AccessToken: token, ExpiresIn: r.fixedLifetime}, nil
}

// systemUserAuthorizationLock embeds pg_advisory_xact_lock in the query's
// ORDER BY expression. Casting PostgreSQL's void result to text yields the same
// empty sort key for every row while guaranteeing the transaction-scoped lock
// statement finishes before the caller performs its authoritative re-read.
func systemUserAuthorizationLock(systemUserID int64) user.OrderOption {
	return func(selector *entsql.Selector) {
		selector.OrderExpr(entsql.Expr("pg_advisory_xact_lock(?)::text", systemUserID))
	}
}

func loadAuthorizationSnapshot(ctx context.Context, tx *ent.Tx, systemUserID int64, userName string) (*biz.AuthorizationSnapshot, error) {
	roles, err := tx.Role.Query().
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
		menus, err = tx.Menu.Query().
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

func toBizAuthorizationSnapshot(systemUserID int64, userName string, roles []*ent.Role, menus []*ent.Menu) *biz.AuthorizationSnapshot {
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
	return &biz.AuthorizationSnapshot{
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
func loginAndReplaceAuthorizationSnapshot(manager *core.Manager, snapshot *biz.AuthorizationSnapshot, deviceType string, lifetime time.Duration) (string, error) {
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
		satokenmanager.SessionKeyRoles:       append([]string(nil), snapshot.RoleKeys...),
		satokenmanager.SessionKeyPermissions: append([]string(nil), snapshot.Permissions...),
	}, lifetime)
	if err != nil {
		_ = manager.LogoutByToken(token)
		return "", err
	}
	return token, nil
}

func authDatabaseBackendError(operation string, err error) error {
	slog.Warn("authentication database operation failed", "op", operation, "err", err)
	return biz.ErrAuthBackendUnavailable
}

func (r *authLoginRepo) cleanupUnregisteredToken(ctx context.Context, token string, systemUserID int64) {
	_ = r.manager.LogoutByToken(token)
	remaining, err := r.sessions.ListBySystemUser(ctx, systemUserID)
	if err == nil && len(remaining) == 0 {
		_ = r.manager.DeleteSession(strconv.FormatInt(systemUserID, 10))
	}
}
