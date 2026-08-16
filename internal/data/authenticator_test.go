package data

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"glex/internal/biz"

	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
	"github.com/sa-tokens/sa-token-go/core/manager"
	satokensession "github.com/sa-tokens/sa-token-go/core/session"
)

func TestAuthenticatorBuildsPrincipalAndRefreshesActivity(t *testing.T) {
	sessions, tokenManager, rdb := newTestLoginSessionRepo(t)
	token, err := loginAndReplaceAuthorizationSnapshot(tokenManager, &biz.AuthorizationSnapshot{
		SystemUserID: 42,
		UserName:     "alice",
		RoleKeys:     []string{"operator"},
		Permissions:  []string{"system:user:list"},
	}, "desktop", 7*24*time.Hour)
	if err != nil {
		t.Fatalf("issue bearer: %v", err)
	}
	loginSession, err := sessions.Create(t.Context(), token, &biz.LoginSession{
		SystemUserID: 42,
		UserName:     "alice",
		DeviceType:   "desktop",
		LoginTime:    time.Now(),
	})
	if err != nil {
		t.Fatalf("create Login Session: %v", err)
	}

	info, err := tokenManager.GetTokenInfo(token)
	if err != nil {
		t.Fatalf("GetTokenInfo: %v", err)
	}
	info.ActiveTime = time.Now().Add(-time.Minute).Unix()
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal TokenInfo: %v", err)
	}
	key := "glex:auth:" + authManagerKeyPrefix + manager.TokenKeyPrefix + token
	if err := rdb.Set(t.Context(), key, encoded, 7*24*time.Hour).Err(); err != nil {
		t.Fatalf("set old active time: %v", err)
	}

	principal, err := NewAuthenticator(tokenManager, sessions).Authenticate(t.Context(), token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.SystemUserID() != 42 || principal.UserName() != "alice" || principal.SessionID() != loginSession.ID {
		t.Fatalf("Principal identity = %d/%q/%q", principal.SystemUserID(), principal.UserName(), principal.SessionID())
	}
	if got := principal.RoleKeys(); len(got) != 1 || got[0] != "operator" {
		t.Fatalf("Role keys = %v", got)
	}
	if got := principal.Permissions(); len(got) != 1 || got[0] != "system:user:list" {
		t.Fatalf("permissions = %v", got)
	}
	updated, err := tokenManager.GetTokenInfo(token)
	if err != nil || updated.ActiveTime <= info.ActiveTime {
		t.Fatalf("active time = %d/%v, want after %d", updated.ActiveTime, err, info.ActiveTime)
	}
}

func TestAuthenticatorPreservesSessionFailureDistinctions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(biz.LoginSessionRepo, string, string) error
		want   error
	}{
		{name: "invalid", mutate: func(biz.LoginSessionRepo, string, string) error { return nil }, want: biz.ErrTokenInvalid},
		{name: "idle", mutate: makeTokenIdle, want: biz.ErrTokenIdleTimeout},
		{name: "kickout", mutate: func(repo biz.LoginSessionRepo, _ string, sessionID string) error {
			return repo.Kickout(t.Context(), sessionID)
		}, want: biz.ErrLoginSessionKickedOut},
		{name: "replaced", mutate: func(repo biz.LoginSessionRepo, _ string, sessionID string) error {
			return repo.Replace(t.Context(), sessionID)
		}, want: biz.ErrLoginSessionReplaced},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, tokenManager, rdb := newTestLoginSessionRepo(t)
			token := "unknown-token"
			sessionID := ""
			if tt.name != "invalid" {
				created, issued := createManagedLoginSession(t, sessions, tokenManager, 42, "alice", "desktop", time.Now())
				token, sessionID = issued, created.ID
			}
			if tt.name == "idle" {
				if err := makeManagedTokenIdle(t, rdb, tokenManager, token); err != nil {
					t.Fatalf("make token idle: %v", err)
				}
			} else if err := tt.mutate(sessions, token, sessionID); err != nil {
				t.Fatalf("mutate token state: %v", err)
			}
			_, err := NewAuthenticator(tokenManager, sessions).Authenticate(t.Context(), token)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Authenticate error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAuthenticatorDistinguishesFixedExpiryFromUnknownToken(t *testing.T) {
	sessions, tokenManager, rdb := newTestLoginSessionRepo(t)
	_, token := createManagedLoginSession(t, sessions, tokenManager, 42, "alice", "desktop", time.Now())
	tokenKey := "glex:auth:" + authManagerKeyPrefix + manager.TokenKeyPrefix + token
	fingerprintKey := "glex:auth:" + loginSessionTokenKeyPrefix + tokenFingerprint(token)
	tokenTTL, err := rdb.PTTL(t.Context(), tokenKey).Result()
	if err != nil {
		t.Fatalf("token PTTL: %v", err)
	}
	fingerprintTTL, err := rdb.PTTL(t.Context(), fingerprintKey).Result()
	if err != nil {
		t.Fatalf("fingerprint PTTL: %v", err)
	}
	if fingerprintTTL <= tokenTTL {
		t.Fatalf("expiry tombstone TTL = %s, want longer than token TTL %s", fingerprintTTL, tokenTTL)
	}
	if err := rdb.Del(t.Context(), tokenKey).Err(); err != nil {
		t.Fatalf("simulate fixed expiry: %v", err)
	}
	_, err = NewAuthenticator(tokenManager, sessions).Authenticate(t.Context(), token)
	if !errors.Is(err, biz.ErrTokenExpired) {
		t.Fatalf("fixed-expired Authenticate error = %v, want ErrTokenExpired", err)
	}
	_, err = NewAuthenticator(tokenManager, sessions).Authenticate(t.Context(), "unknown-token")
	if !errors.Is(err, biz.ErrTokenInvalid) {
		t.Fatalf("unknown Authenticate error = %v, want ErrTokenInvalid", err)
	}
}

func makeTokenIdle(biz.LoginSessionRepo, string, string) error { return nil }

func makeManagedTokenIdle(t *testing.T, rdb *redis.Client, tokenManager *core.Manager, token string) error {
	t.Helper()
	info, err := tokenManager.GetTokenInfo(token)
	if err != nil {
		return err
	}
	info.ActiveTime = time.Now().Add(-time.Hour).Unix()
	encoded, err := json.Marshal(info)
	if err != nil {
		return err
	}
	key := "glex:auth:" + authManagerKeyPrefix + manager.TokenKeyPrefix + token
	return rdb.Set(t.Context(), key, encoded, 7*24*time.Hour).Err()
}

func TestAuthenticatorRejectsMalformedOrMismatchedAuthorizationSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*satokensession.Session) error
	}{
		{name: "missing username", mutate: func(session *satokensession.Session) error { return session.Delete(authorizationSnapshotUserNameKey) }},
		{name: "wrong System User ID", mutate: func(session *satokensession.Session) error {
			return session.Set(authorizationSnapshotSystemUserIDKey, strconv.FormatInt(84, 10))
		}},
		{name: "malformed Roles", mutate: func(session *satokensession.Session) error { return session.Set(manager.SessionKeyRoles, "operator") }},
		{name: "malformed permissions", mutate: func(session *satokensession.Session) error {
			return session.Set(manager.SessionKeyPermissions, []any{"system:user:list", 7})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, tokenManager, _ := newTestLoginSessionRepo(t)
			_, token := createManagedLoginSession(t, sessions, tokenManager, 42, "alice", "desktop", time.Now())
			accountSession, err := tokenManager.GetSession("42")
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if err := tt.mutate(accountSession); err != nil {
				t.Fatalf("mutate snapshot: %v", err)
			}
			_, err = NewAuthenticator(tokenManager, sessions).Authenticate(t.Context(), token)
			if !errors.Is(err, biz.ErrAuthBackendUnavailable) {
				t.Fatalf("Authenticate error = %v, want ErrAuthBackendUnavailable", err)
			}
		})
	}
}
