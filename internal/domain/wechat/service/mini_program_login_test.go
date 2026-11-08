package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/pkg/listq"
)

func TestMiniProgramLoginReturnsOpenidWithoutSessionOrPerson(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	before := describeApp(mustGetApp(t, f, held.App.ID))
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	f.api.miniLogin = MiniProgramLoginResult{Class: CallSuccess, OpenID: "o-direct-openid-99"}

	got, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{
		AppID: held.App.AppID,
		Code:  "login-code-once",
	})
	if err != nil {
		t.Fatalf("mini program login: %v", err)
	}
	if got == nil || got.OpenID != "o-direct-openid-99" || got.UnionID != "" {
		t.Fatalf("identity = %+v, want openid o-direct-openid-99 and an empty unionid", got)
	}
	rendered := strings.ToLower(fmt.Sprintf("%+v", got))
	if strings.Contains(rendered, "session") || strings.Contains(rendered, "credential") || strings.Contains(rendered, "token") {
		t.Fatalf("identity carries a session or a system credential: %+v", got)
	}
	if describeApp(mustGetApp(t, f, held.App.ID)) != before || mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("mini program login changed or lapsed the app")
	}
	after, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != len(listed) {
		t.Fatalf("login stored a row: before %d after %d", len(listed), len(after))
	}
	if strings.Contains(describeApp(mustGetApp(t, f, held.App.ID)), "o-direct-openid-99") {
		t.Fatal("openid was stored on the app")
	}
}

func TestMiniProgramLoginReturnsUnionidWhenWeChatSuppliesOne(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	before := describeApp(mustGetApp(t, f, held.App.ID))
	f.api.miniLogin = MiniProgramLoginResult{
		Class:   CallSuccess,
		OpenID:  "o-direct-openid-99",
		UnionID: "u-union-77",
	}

	got, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{
		AppID: held.App.AppID,
		Code:  "login-code-with-union",
	})
	if err != nil {
		t.Fatalf("mini program login: %v", err)
	}
	if got == nil || got.OpenID != "o-direct-openid-99" || got.UnionID != "u-union-77" {
		t.Fatalf("identity = %+v, want openid and unionid", got)
	}
	if strings.Contains(strings.ToLower(fmt.Sprintf("%+v", got)), "session") {
		t.Fatalf("identity carries a session: %+v", got)
	}
	if describeApp(mustGetApp(t, f, held.App.ID)) != before || strings.Contains(describeApp(mustGetApp(t, f, held.App.ID)), "u-union-77") {
		t.Fatal("unionid was stored on the app")
	}
}

func TestMiniProgramLoginBusinessRejectionDoesNotLapse(t *testing.T) {
	classes := []CallClass{CallBusinessRejection, CallUnavailable}
	for _, class := range classes {
		f := newAuthFixture(t)
		held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
		if err != nil {
			t.Fatalf("direct hold: %v", err)
		}
		before := describeApp(mustGetApp(t, f, held.App.ID))
		f.api.miniLogin = MiniProgramLoginResult{Class: class}

		got, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{
			AppID: held.App.AppID,
			Code:  "login-code-rejected",
		})
		if !errors.Is(err, wechat.ErrWechatRejected) || got != nil {
			t.Fatalf("class %d = (%+v, %v), want rejection without an identity", class, got, err)
		}
		if describeApp(mustGetApp(t, f, held.App.ID)) != before || mustGetApp(t, f, held.App.ID).Lapsed {
			t.Fatalf("class %d lapsed or changed the app", class)
		}
	}
}

func TestMiniProgramLoginDefinitiveRefusalLapsesTheApp(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.miniLogin = MiniProgramLoginResult{Class: CallDefinitiveRefusal}

	for _, appID := range []string{granted.AppID, held.App.AppID} {
		got, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{
			AppID: appID,
			Code:  "login-code-refused",
		})
		if !errors.Is(err, wechat.ErrWechatRefused) || got != nil {
			t.Fatalf("refusal of %s = (%+v, %v), want no identity", appID, got, err)
		}
	}
	if got := mustGetApp(t, f, granted.ID); !got.Lapsed || got.Connection != entity.ConnectionPlatformAuthorization || got.PlatformID != platform.ID || got.Suspended || got.UpdateBy != 0 {
		t.Fatalf("platform app after refusal = %+v", got)
	}
	if got := mustGetApp(t, f, held.App.ID); !got.Lapsed || got.Connection != entity.ConnectionDirectHold || got.PlatformID != 0 || got.Suspended || got.UpdateBy != 0 {
		t.Fatalf("direct hold after refusal = %+v", got)
	}
	if strings.Contains(describeApp(mustGetApp(t, f, granted.ID))+describeApp(mustGetApp(t, f, held.App.ID)), "login-code-refused") {
		t.Fatal("refusal stored the login code")
	}
}

func TestMiniProgramLoginUsesOneEntryForDirectHoldAndPlatformAuthorization(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	beforeGranted := describeApp(mustGetApp(t, f, granted.ID))
	beforeHeld := describeApp(mustGetApp(t, f, held.App.ID))

	f.api.miniLogin = MiniProgramLoginResult{Class: CallSuccess, OpenID: "o-platform-openid"}
	platformID, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{
		AppID: granted.AppID,
		Code:  "code-platform",
	})
	if err != nil {
		t.Fatalf("platform login: %v", err)
	}
	f.api.miniLogin = MiniProgramLoginResult{Class: CallSuccess, OpenID: "o-direct-openid", UnionID: "u-direct-union"}
	directID, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{
		AppID: held.App.AppID,
		Code:  "code-direct",
	})
	if err != nil {
		t.Fatalf("direct login: %v", err)
	}
	if platformID == nil || platformID.OpenID != "o-platform-openid" || platformID.UnionID != "" {
		t.Fatalf("platform identity = %+v", platformID)
	}
	if directID == nil || directID.OpenID != "o-direct-openid" || directID.UnionID != "u-direct-union" {
		t.Fatalf("direct identity = %+v", directID)
	}
	if len(f.api.miniLogins) != 2 {
		t.Fatalf("exchanges = %d, want one per app", len(f.api.miniLogins))
	}
	plat := f.api.miniLogins[0]
	if plat.Connection != entity.ConnectionPlatformAuthorization || plat.AppID != granted.AppID || plat.Code != "code-platform" || plat.AppSecret != "" || plat.ComponentAppID != "wxcompappid000001" || plat.ComponentSecret != testComponentSecret || plat.VerifyTicket != "ticket-wxcompappid000001" {
		t.Fatalf("platform exchange = %+v", plat)
	}
	direct := f.api.miniLogins[1]
	if direct.Connection != entity.ConnectionDirectHold || direct.AppID != held.App.AppID || direct.Code != "code-direct" || direct.AppSecret != testAppSecret || direct.ComponentAppID != "" {
		t.Fatalf("direct exchange = %+v", direct)
	}
	for _, call := range f.api.miniLogins {
		rendered := strings.ToLower(fmt.Sprintf("%+v", call))
		if strings.Contains(rendered, "session") {
			t.Fatalf("exchange carries a session: %+v", call)
		}
	}
	if describeApp(mustGetApp(t, f, granted.ID)) != beforeGranted || describeApp(mustGetApp(t, f, held.App.ID)) != beforeHeld {
		t.Fatal("login changed an app")
	}
}

func TestMiniProgramLoginRefusesUnusableAppsWithoutStoringAPerson(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	unready := f.holdReady(t, "华南平台", "wxcompappid000002")
	live := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	suspendedGrant := f.grantMini(t, platform.ID, "wxminiappid000098", []string{entity.CapabilityMiniProgramLogin})
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: suspendedGrant.ID, Suspended: true, ActorID: 9}); err != nil {
		t.Fatalf("suspend grant: %v", err)
	}
	missing := f.grantMini(t, platform.ID, "wxminiappid000088", []string{entity.CapabilitySubscribeMessage})
	unreadyApp := f.grantMini(t, unready.ID, "wxminiappid000077", []string{entity.CapabilityMiniProgramLogin})
	if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), unready.ID); err != nil {
		t.Fatalf("release ticket: %v", err)
	}
	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000099",
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityWebAuthorization},
		RefreshToken: "authorizer-refresh-SSSS",
	})
	officialGrant, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-service",
	})
	if err != nil {
		t.Fatalf("service grant: %v", err)
	}
	official := miniHold("wxofficial000001", 7)
	official.Name = "服务号甲"
	official.AppType = entity.AppTypeOfficialAccount
	official.Kind = entity.AccountKindService
	officialHold, err := f.apps.Create(context.Background(), official)
	if err != nil {
		t.Fatalf("official hold: %v", err)
	}
	suspendedHold, err := f.apps.Create(context.Background(), miniHold("wxminiappid000002", 7))
	if err != nil {
		t.Fatalf("suspended hold: %v", err)
	}
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: suspendedHold.App.ID, Suspended: true, ActorID: 8}); err != nil {
		t.Fatalf("suspend hold: %v", err)
	}
	withdrawn, err := f.apps.Create(context.Background(), miniHold("wxminiappid000003", 7))
	if err != nil {
		t.Fatalf("withdrawn hold: %v", err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: withdrawn.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	lapsed, err := f.apps.Create(context.Background(), miniHold("wxminiappid000004", 7))
	if err != nil {
		t.Fatalf("lapsed hold: %v", err)
	}
	f.api.miniLogin = MiniProgramLoginResult{Class: CallDefinitiveRefusal}
	if _, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{AppID: lapsed.App.AppID, Code: "code-lapse"}); !errors.Is(err, wechat.ErrWechatRefused) || !mustGetApp(t, f, lapsed.App.ID).Lapsed {
		t.Fatalf("setup lapse: %v", err)
	}

	f.api.miniLogin = MiniProgramLoginResult{Class: CallSuccess, OpenID: "o-must-not-be-stored", UnionID: "u-must-not-be-stored"}
	calls := len(f.api.miniLogins)
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	cases := []struct {
		name  string
		appID string
		want  error
	}{
		{name: "unknown", appID: "wxminiappid000404", want: wechat.ErrAppNotFound},
		{name: "withdrawn", appID: withdrawn.App.AppID, want: wechat.ErrAppNotFound},
		{name: "suspended grant", appID: suspendedGrant.AppID, want: wechat.ErrAppSuspended},
		{name: "suspended hold", appID: suspendedHold.App.AppID, want: wechat.ErrAppSuspended},
		{name: "lapsed", appID: lapsed.App.AppID, want: wechat.ErrAppLapsed},
		{name: "official hold", appID: officialHold.App.AppID, want: wechat.ErrAppInvalid},
		{name: "official grant", appID: officialGrant.AppID, want: wechat.ErrAppInvalid},
		{name: "missing login capability", appID: missing.AppID, want: wechat.ErrAppCapability},
		{name: "unready platform", appID: unreadyApp.AppID, want: wechat.ErrPlatformNotReady},
	}
	for _, tc := range cases {
		got, err := f.apps.MiniProgramLogin(context.Background(), MiniProgramLoginRequest{AppID: tc.appID, Code: "code-" + tc.name})
		if !errors.Is(err, tc.want) || got != nil {
			t.Fatalf("%s = (%+v, %v), want %v", tc.name, got, err, tc.want)
		}
	}
	if len(f.api.miniLogins) != calls {
		t.Fatalf("refused logins called WeChat %d extra times", len(f.api.miniLogins)-calls)
	}
	after, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != len(listed) {
		t.Fatalf("refusals stored a row: before %d after %d", len(listed), len(after))
	}
	for _, app := range after {
		rendered := describeApp(app)
		if strings.Contains(rendered, "o-must-not-be-stored") || strings.Contains(rendered, "u-must-not-be-stored") || strings.Contains(rendered, "code-") {
			t.Fatalf("refusal stored a person on %s", rendered)
		}
	}
	if !mustGetApp(t, f, lapsed.App.ID).Lapsed {
		t.Fatal("lapsed app was restored")
	}
	for _, id := range []int64{live.ID, suspendedGrant.ID, missing.ID, unreadyApp.ID, officialGrant.ID, officialHold.App.ID, suspendedHold.App.ID} {
		if mustGetApp(t, f, id).Lapsed {
			t.Fatalf("app %d lapsed on a refused login", id)
		}
	}
}
