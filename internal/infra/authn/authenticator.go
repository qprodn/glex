package authninfra

import (
	"context"
	"errors"
	"strconv"

	authndomain "glex/internal/domain/authn"

	"github.com/sa-tokens/sa-token-go/core"
	satokenmanager "github.com/sa-tokens/sa-token-go/core/manager"
)

type authenticator struct {
	manager  *core.Manager
	sessions authndomain.LoginSessionRepo
}

// NewAuthenticator constructs the transport-neutral authentication capability.
func NewAuthenticator(manager *core.Manager, sessions authndomain.LoginSessionRepo) authndomain.Authenticator {
	return &authenticator{manager: manager, sessions: sessions}
}

func (a *authenticator) Authenticate(ctx context.Context, bearerToken string) (*authndomain.Principal, error) {
	loginSession, err := a.sessions.FindByToken(ctx, bearerToken)
	if err != nil {
		if errors.Is(err, authndomain.ErrLoginSessionNotFound) {
			return nil, authndomain.ErrTokenInvalid
		}
		return nil, err
	}
	loginID := strconv.FormatInt(loginSession.SystemUserID, 10)
	accountSession, err := a.manager.GetSession(loginID)
	if err != nil {
		return nil, mapLoginSessionAuthError(err)
	}
	storedID, idOK := accountSession.Get(authorizationSnapshotSystemUserIDKey)
	storedName, nameOK := accountSession.Get(authorizationSnapshotUserNameKey)
	roles, rolesOK := snapshotStrings(accountSession, satokenmanager.SessionKeyRoles)
	permissions, permissionsOK := snapshotStrings(accountSession, satokenmanager.SessionKeyPermissions)
	if !idOK || storedID != loginID || !nameOK || storedName != loginSession.UserName || !rolesOK || !permissionsOK {
		return nil, authndomain.ErrAuthBackendUnavailable
	}
	// FindByToken performs the explicit idle-timeout check. Touch activity only
	// after the identity and authorization snapshot have been decoded safely.
	if err := a.manager.UpdateLastActiveToNow(bearerToken); err != nil {
		return nil, mapLoginSessionAuthError(err)
	}
	return authndomain.NewPrincipal(loginSession.SystemUserID, loginSession.UserName, loginSession.ID, roles, permissions), nil
}

type sessionValueReader interface {
	Get(string) (any, bool)
}

func snapshotStrings(session sessionValueReader, key string) ([]string, bool) {
	value, ok := session.Get(key)
	if !ok {
		return nil, false
	}
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		result := make([]string, len(values))
		for i, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[i] = text
		}
		return result, true
	default:
		return nil, false
	}
}
