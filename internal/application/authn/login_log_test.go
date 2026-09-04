package authn

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"

	authnv1 "glex/api/authn/v1"
)

// The Login Log capture tests (ADR 0012): every login outcome — success,
// password failure, commit failure — records one row, and every completed
// revocation (logout, 强退, self-revoke) records its session-end row. A
// recording failure never changes the operation's outcome.

func TestLoginRecordsSuccessAndFailureRows(t *testing.T) {
	login := &fakeLoginService{result: &authnentity.LoginResult{AccessToken: "secret"}}
	app, writer := newLoginAppWithWriter(login, newFakeTransaction())

	if _, err := app.Login(t.Context(), &authnv1.LoginRequest{Username: "alice", Password: "correct"}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(writer.logs) != 1 || !writer.logs[0].Success || writer.logs[0].UserName != "alice" {
		t.Fatalf("success logs = %+v, want one success row for alice", writer.logs)
	}
	if writer.logs[0].Msg != "登录成功" || writer.logs[0].UserID != 42 {
		t.Fatalf("success row = %+v, want 登录成功 for user 42", writer.logs[0])
	}

	login.verifyErr = authndomain.ErrInvalidCredentials
	_, _ = app.Login(t.Context(), &authnv1.LoginRequest{Username: "mallory", Password: "wrong"})
	last := writer.logs[len(writer.logs)-1]
	if last.Success || last.UserName != "mallory" || last.UserID != 0 {
		t.Fatalf("failure row = %+v, want a failure row for mallory without an account id", last)
	}
	if last.Msg == "" {
		t.Fatal("failure row carries no cause text")
	}
}

func TestLoginFailureRecordingNeverFailsTheLogin(t *testing.T) {
	login := &fakeLoginService{verifyErr: authndomain.ErrInvalidCredentials}
	app, writer := newLoginAppWithWriter(login, newFakeTransaction())
	writer.err = errors.New("audit store down")

	_, err := app.Login(t.Context(), &authnv1.LoginRequest{Username: "alice", Password: "nope"})
	if !errors.Is(err, authndomain.ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if len(writer.logs) != 0 {
		t.Fatalf("failed writes recorded rows: %+v", writer.logs)
	}
}

func TestKickoutRecordsForcedLogoutRow(t *testing.T) {
	loginTime := time.Unix(1_700_000_000, 0)
	svc := &fakeOnlineSessionService{sessions: []*authnentity.LoginSession{{
		ID:           "session-id",
		SystemUserID: 42,
		UserName:     "alice",
		IPAddress:    "192.0.2.10",
		LoginTime:    loginTime,
	}}}
	writer := &fakeLoginLogWriter{}
	app := NewOnlineSessionApplication(svc, writer, testLogger())

	if _, err := app.KickoutOnlineSession(principalCtx(t, 7, "admin-session"), &authnv1.OnlineSessionRequest{TokenId: "session-id"}); err != nil {
		t.Fatalf("KickoutOnlineSession: %v", err)
	}
	if len(writer.logs) != 1 {
		t.Fatalf("logs = %+v, want exactly the 强退 row", writer.logs)
	}
	row := writer.logs[0]
	if row.Msg != "强制退出" || row.UserName != "alice" || row.UserID != 42 || row.ActorID != 7 {
		t.Fatalf("强退 row = %+v, want alice/42 kicked by admin 7", row)
	}
}

func TestLogoutAndSelfRevokeRecordSessionEndRows(t *testing.T) {
	login := &fakeLoginService{}
	writer := &fakeLoginLogWriter{}
	app := NewLoginApplication(login, nil, writer, newFakeTransaction(), testLogger())

	if _, err := app.Logout(principalCtx(t, 42, "current-session"), &emptypb.Empty{}); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(writer.logs) != 1 || writer.logs[0].Msg != "退出登录" {
		t.Fatalf("logout rows = %+v, want one 退出登录 row", writer.logs)
	}
}
