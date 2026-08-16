package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authv1 "glex/api/system/auth/v1"
	"glex/internal/biz"
	"glex/internal/service"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"golang.org/x/crypto/bcrypt"
)

type routeUserRepo struct {
	biz.UserRepo
	user *biz.User
}

func (r *routeUserRepo) FindByUserName(context.Context, string) (*biz.User, error) {
	return r.user, nil
}

type routeLoginRepo struct {
	result *biz.LoginResult
	issues []*biz.LoginIssue
}

func (r *routeLoginRepo) Issue(_ context.Context, issue *biz.LoginIssue) (*biz.LoginResult, error) {
	r.issues = append(r.issues, issue)
	return r.result, nil
}

type routeCounterRepo struct{}

func (*routeCounterRepo) LockRemaining(context.Context, string, string) (time.Duration, error) {
	return 0, nil
}
func (*routeCounterRepo) RecordFailure(context.Context, string, string) (time.Duration, error) {
	return 0, nil
}
func (*routeCounterRepo) ClearUserName(context.Context, string) error { return nil }

type routeAuthenticator struct {
	principal *biz.Principal
}

func (a *routeAuthenticator) Authenticate(_ context.Context, bearerToken string) (*biz.Principal, error) {
	switch bearerToken {
	case "route-bearer-secret":
		return a.principal, nil
	case "expired-bearer-secret":
		return nil, biz.ErrTokenExpired
	default:
		return nil, biz.ErrTokenInvalid
	}
}

type routeSessionRepo struct {
	biz.LoginSessionRepo
	sessions       []*biz.LoginSession
	logoutIDs      []string
	kickoutIDs     []string
	ownedUserID    int64
	ownedSessionID string
	err            error
}

func (r *routeSessionRepo) List(context.Context, ...biz.ListOption) ([]*biz.LoginSession, error) {
	return r.sessions, nil
}

func (r *routeSessionRepo) ListBySystemUser(_ context.Context, systemUserID int64, _ ...biz.ListOption) ([]*biz.LoginSession, error) {
	result := make([]*biz.LoginSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		if session.SystemUserID == systemUserID {
			result = append(result, session)
		}
	}
	return result, nil
}

func (r *routeSessionRepo) Logout(_ context.Context, sessionID string) error {
	r.logoutIDs = append(r.logoutIDs, sessionID)
	return r.err
}

func (r *routeSessionRepo) Kickout(_ context.Context, sessionID string) error {
	r.kickoutIDs = append(r.kickoutIDs, sessionID)
	return r.err
}

func (r *routeSessionRepo) LogoutOwned(_ context.Context, systemUserID int64, sessionID string) error {
	if systemUserID == 42 && sessionID == "other-session" {
		return biz.ErrLoginSessionNotOwned
	}
	r.ownedUserID = systemUserID
	r.ownedSessionID = sessionID
	return r.err
}

func TestRuoYiAuthAndOnlineSessionRoutesAndPolicies(t *testing.T) {
	server, loginRepo, sessions := newRouteTestServer(t, biz.NewPrincipal(
		42,
		"alice",
		"current-session",
		nil,
		[]string{"monitor:online:list", "monitor:online:forceLogout"},
	))

	login := routeRequest(t, server, http.MethodPost, "/auth/login", `{"username":"alice","password":"correct"}`, "not-a-bearer")
	if login.Code != http.StatusOK {
		t.Fatalf("POST /auth/login status = %d, body = %s", login.Code, login.Body.String())
	}
	var loginReply authv1.LoginReply
	if err := json.Unmarshal(login.Body.Bytes(), &loginReply); err != nil {
		t.Fatalf("decode Login reply: %v", err)
	}
	if loginReply.GetAccessToken() != "login-bearer-secret" || loginReply.GetExpireIn() != int64((7*24*time.Hour)/time.Second) {
		t.Fatalf("Login reply = %#v", &loginReply)
	}
	if len(loginRepo.issues) != 1 || loginRepo.issues[0].SourceIP != "192.0.2.10" || loginRepo.issues[0].DeviceType != "mobile" {
		t.Fatalf("Login issue metadata = %#v", loginRepo.issues)
	}

	logout := routeRequest(t, server, http.MethodPost, "/auth/logout", `{}`, "Bearer route-bearer-secret")
	if logout.Code != http.StatusOK || len(sessions.logoutIDs) != 1 || sessions.logoutIDs[0] != "current-session" {
		t.Fatalf("POST /auth/logout = %d, Logout IDs = %v, body = %s", logout.Code, sessions.logoutIDs, logout.Body.String())
	}
	anonymousLogout := routeRequest(t, server, http.MethodPost, "/auth/logout", `{}`, "")
	if anonymousLogout.Code != http.StatusOK || len(sessions.logoutIDs) != 1 {
		t.Fatalf("anonymous POST /auth/logout = %d, Logout IDs = %v", anonymousLogout.Code, sessions.logoutIDs)
	}
	for _, credential := range []string{"invalid-bearer-secret", "expired-bearer-secret"} {
		response := routeRequest(t, server, http.MethodPost, "/auth/logout", `{}`, "Bearer "+credential)
		if response.Code != http.StatusOK || len(sessions.logoutIDs) != 1 {
			t.Fatalf("OPTIONAL POST /auth/logout with %q = %d, Logout IDs = %v", credential, response.Code, sessions.logoutIDs)
		}
	}

	globalList := routeRequest(t, server, http.MethodGet, "/monitor/online/list", "", "Bearer route-bearer-secret")
	if globalList.Code != http.StatusOK {
		t.Fatalf("GET /monitor/online/list status = %d, body = %s", globalList.Code, globalList.Body.String())
	}
	var globalReply authv1.OnlineSessionSet
	if err := json.Unmarshal(globalList.Body.Bytes(), &globalReply); err != nil {
		t.Fatalf("decode online list: %v; body = %s", err, globalList.Body.String())
	}
	if len(globalReply.GetSessions()) != 2 || globalReply.GetSessions()[0].GetTokenId() != "current-session" {
		t.Fatalf("global online list = %#v", &globalReply)
	}
	if strings.Contains(globalList.Body.String(), "route-bearer-secret") || strings.Contains(globalList.Body.String(), "login-bearer-secret") {
		t.Fatal("online list exposed the bearer credential")
	}
	for _, session := range globalReply.GetSessions() {
		replay := routeRequest(t, server, http.MethodGet, "/monitor/online", "", "Bearer "+session.GetTokenId())
		if replay.Code != http.StatusUnauthorized {
			t.Fatalf("Session ID %q was reusable as a bearer credential: status %d", session.GetTokenId(), replay.Code)
		}
	}

	kickout := routeRequest(t, server, http.MethodDelete, "/monitor/online/other-session", "", "Bearer route-bearer-secret")
	if kickout.Code != http.StatusOK || len(sessions.kickoutIDs) != 1 || sessions.kickoutIDs[0] != "other-session" {
		t.Fatalf("DELETE /monitor/online/{tokenId} = %d, IDs = %v", kickout.Code, sessions.kickoutIDs)
	}

	myList := routeRequest(t, server, http.MethodGet, "/monitor/online", "", "Bearer route-bearer-secret")
	if myList.Code != http.StatusOK {
		t.Fatalf("GET /monitor/online status = %d, body = %s", myList.Code, myList.Body.String())
	}
	var myReply authv1.OnlineSessionSet
	if err := json.Unmarshal(myList.Body.Bytes(), &myReply); err != nil {
		t.Fatalf("decode my online list: %v; body = %s", err, myList.Body.String())
	}
	if len(myReply.GetSessions()) != 1 || myReply.GetSessions()[0].GetUserName() != "alice" {
		t.Fatalf("current-user list = %#v", &myReply)
	}

	removeMine := routeRequest(t, server, http.MethodDelete, "/monitor/online/myself/current-session", "", "Bearer route-bearer-secret")
	if removeMine.Code != http.StatusOK || sessions.ownedUserID != 42 || sessions.ownedSessionID != "current-session" {
		t.Fatalf("DELETE /monitor/online/myself/{tokenId} = %d, ownership = %d/%q", removeMine.Code, sessions.ownedUserID, sessions.ownedSessionID)
	}
}

func TestOnlineSessionRoutesRejectMissingOrInsufficientCredentials(t *testing.T) {
	server, _, _ := newRouteTestServer(t, biz.NewPrincipal(42, "alice", "session-id", nil, nil))
	tests := []struct {
		name          string
		method        string
		path          string
		authorization string
		wantStatus    int
	}{
		{name: "global list missing", method: http.MethodGet, path: "/monitor/online/list", wantStatus: http.StatusUnauthorized},
		{name: "kickout missing", method: http.MethodDelete, path: "/monitor/online/session-id", wantStatus: http.StatusUnauthorized},
		{name: "my list missing", method: http.MethodGet, path: "/monitor/online", wantStatus: http.StatusUnauthorized},
		{name: "my delete missing", method: http.MethodDelete, path: "/monitor/online/myself/session-id", wantStatus: http.StatusUnauthorized},
		{name: "global list permission", method: http.MethodGet, path: "/monitor/online/list", authorization: "Bearer route-bearer-secret", wantStatus: http.StatusForbidden},
		{name: "kickout permission", method: http.MethodDelete, path: "/monitor/online/session-id", authorization: "Bearer route-bearer-secret", wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := routeRequest(t, server, tt.method, tt.path, "", tt.authorization)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestLogoutRedisFailureReturnsServiceUnavailable(t *testing.T) {
	server, _, sessions := newRouteTestServer(t, biz.NewPrincipal(42, "alice", "session-id", nil, nil))
	sessions.err = biz.ErrAuthBackendUnavailable
	response := routeRequest(t, server, http.MethodPost, "/auth/logout", `{}`, "Bearer route-bearer-secret")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /auth/logout status = %d, want 503; body = %s", response.Code, response.Body.String())
	}
}

func TestCurrentSystemUserCannotRemoveAnotherUsersSession(t *testing.T) {
	server, _, sessions := newRouteTestServer(t, biz.NewPrincipal(42, "alice", "session-id", nil, nil))
	response := routeRequest(t, server, http.MethodDelete, "/monitor/online/myself/other-session", "", "Bearer route-bearer-secret")
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign Session removal status = %d, want 403; body = %s", response.Code, response.Body.String())
	}
	if sessions.ownedSessionID != "" {
		t.Fatalf("foreign Session reached revocation: %q", sessions.ownedSessionID)
	}
}

func TestNoBulkOnlineSessionRouteIsRegistered(t *testing.T) {
	server, _, _ := newRouteTestServer(t, biz.NewPrincipal(42, "alice", "session-id", nil, nil))
	got := make(map[string]bool)
	if err := server.WalkRoute(func(route kratoshttp.RouteInfo) error {
		if strings.HasPrefix(route.Path, "/auth/") || strings.HasPrefix(route.Path, "/monitor/online") {
			got[route.Method+" "+route.Path] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkRoute: %v", err)
	}
	want := []string{
		"POST /auth/login",
		"POST /auth/logout",
		"GET /monitor/online/list",
		"DELETE /monitor/online/{token_id}",
		"GET /monitor/online",
		"DELETE /monitor/online/myself/{token_id}",
	}
	if len(got) != len(want) {
		t.Fatalf("registered auth/session routes = %v, want exactly %v", got, want)
	}
	for _, route := range want {
		if !got[route] {
			t.Errorf("required route %q is not registered", route)
		}
	}
}

func newRouteTestServer(t *testing.T, principal *biz.Principal) (*kratoshttp.Server, *routeLoginRepo, *routeSessionRepo) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	loginRepo := &routeLoginRepo{result: &biz.LoginResult{AccessToken: "login-bearer-secret", ExpiresIn: 7 * 24 * time.Hour}}
	sessionRepo := &routeSessionRepo{sessions: []*biz.LoginSession{
		{ID: "current-session", SystemUserID: 42, UserName: "alice", DeviceType: "mobile", IPAddress: "192.0.2.10", Browser: "Chrome", OS: "iOS", LoginTime: time.Unix(1_700_000_000, 0)},
		{ID: "other-session", SystemUserID: 84, UserName: "bob", DeviceType: "desktop", IPAddress: "192.0.2.20", Browser: "Firefox", OS: "Linux", LoginTime: time.Unix(1_700_000_001, 0)},
	}}
	authUsecase := biz.NewAuthUsecase(
		&routeUserRepo{user: &biz.User{ID: 42, UserName: "alice", Password: string(hash)}},
		nil,
		nil,
		loginRepo,
		&routeCounterRepo{},
		sessionRepo,
	)
	sessionUsecase := biz.NewLoginSessionUsecase(sessionRepo)
	authService := service.NewAuthService(authUsecase)
	onlineService := service.NewOnlineSessionService(sessionUsecase)
	descriptor := authv1.File_system_auth_v1_auth_proto.Services()
	policies, err := newAuthPolicyRegistry(
		descriptor.ByName("AuthService"),
		descriptor.ByName("OnlineSessionService"),
	)
	if err != nil {
		t.Fatalf("newAuthPolicyRegistry: %v", err)
	}
	server := kratoshttp.NewServer(kratoshttp.Middleware(
		recovery.Recovery(),
		authMiddleware(&routeAuthenticator{principal: principal}, policies),
		validate.Middleware(),
	))
	authv1.RegisterAuthServiceHTTPServer(server, authService)
	authv1.RegisterOnlineSessionServiceHTTPServer(server, onlineService)
	return server, loginRepo, sessionRepo
}

func routeRequest(t *testing.T, server http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	request.Header.Set("X-Forwarded-For", "192.0.2.10")
	request.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}
