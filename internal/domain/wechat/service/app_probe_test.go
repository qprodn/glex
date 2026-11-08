package service

import (
	"context"
	"errors"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
)

func TestAppProbeSucceedsWithoutChangingSuspensionLapseOrConnection(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: granted.ID, Suspended: true, ActorID: 9}); err != nil {
		t.Fatalf("suspend granted: %v", err)
	}
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: held.App.ID, Suspended: true, ActorID: 8}); err != nil {
		t.Fatalf("suspend held: %v", err)
	}
	beforeGranted := describeApp(mustGetApp(t, f, granted.ID))
	beforeHeld := describeApp(mustGetApp(t, f, held.App.ID))
	refreshBefore, err := f.appRepo.LoadSecrets(context.Background(), granted.ID)
	if err != nil {
		t.Fatalf("load refresh: %v", err)
	}

	for _, id := range []int64{granted.ID, held.App.ID} {
		outcome, err := f.apps.Probe(context.Background(), id)
		if err != nil || outcome == nil || !outcome.OK {
			t.Fatalf("probe %d = (%+v, %v)", id, outcome, err)
		}
	}
	if got := describeApp(mustGetApp(t, f, granted.ID)); got != beforeGranted {
		t.Fatalf("successful probe changed the platform app\n got %s\nwant %s", got, beforeGranted)
	}
	if got := describeApp(mustGetApp(t, f, held.App.ID)); got != beforeHeld {
		t.Fatalf("successful probe changed the direct hold\n got %s\nwant %s", got, beforeHeld)
	}
	refreshAfter, err := f.appRepo.LoadSecrets(context.Background(), granted.ID)
	if err != nil {
		t.Fatalf("load refresh after: %v", err)
	}
	if refreshAfter.AuthorizerRefreshToken != refreshBefore.AuthorizerRefreshToken || refreshAfter.AppSecret != "" {
		t.Fatalf("successful probe rotated material: before %q after %q secret %q", refreshBefore.AuthorizerRefreshToken, refreshAfter.AuthorizerRefreshToken, refreshAfter.AppSecret)
	}
	if len(f.api.appProbes) != 2 {
		t.Fatalf("probes = %d, want one per app and no further call", len(f.api.appProbes))
	}
	plat := f.api.appProbes[0]
	if plat.Connection != entity.ConnectionPlatformAuthorization || plat.AppID != granted.AppID || plat.AppSecret != "" || plat.ComponentAppID != platform.ComponentAppID || plat.AuthorizerRefreshToken != refreshBefore.AuthorizerRefreshToken {
		t.Fatalf("platform probe request = %+v", plat)
	}
	direct := f.api.appProbes[1]
	if direct.Connection != entity.ConnectionDirectHold || direct.AppID != held.App.AppID || direct.AppSecret != testAppSecret || direct.AuthorizerRefreshToken != "" || direct.ComponentAppID != "" {
		t.Fatalf("direct probe request = %+v", direct)
	}
	if mustGetApp(t, f, granted.ID).Lapsed || mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("successful probe lapsed an app")
	}
}

func TestSuccessfulPlatformProbeStoresRotatedRefreshToken(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: granted.ID, Suspended: true, ActorID: 9}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallSuccess, Message: "连接成功", AuthorizerRefreshToken: "authorizer-refresh-ROTATED"}
	outcome, err := f.apps.Probe(context.Background(), granted.ID)
	if err != nil || outcome == nil || !outcome.OK {
		t.Fatalf("probe = (%+v, %v)", outcome, err)
	}
	got := mustGetApp(t, f, granted.ID)
	if got.ID != granted.ID || got.PlatformID != platform.ID || got.Connection != entity.ConnectionPlatformAuthorization || !got.Suspended || got.Lapsed {
		t.Fatalf("rotated refresh changed the app: %+v", got)
	}
	secrets, err := f.appRepo.LoadSecrets(context.Background(), granted.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if secrets.AuthorizerRefreshToken != "authorizer-refresh-ROTATED" || secrets.AppSecret != "" {
		t.Fatalf("refresh = %q app secret %q", secrets.AuthorizerRefreshToken, secrets.AppSecret)
	}
}

func TestAppProbeDefinitiveRefusalLapsesTheSameApp(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin, entity.CapabilitySubscribeMessage})
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "授权已被取消"}

	for _, id := range []int64{granted.ID, held.App.ID} {
		outcome, err := f.apps.Probe(context.Background(), id)
		if err != nil || outcome == nil || outcome.OK {
			t.Fatalf("probe %d = (%+v, %v), want a failed test", id, outcome, err)
		}
		if outcome.Message == "已受理" {
			t.Fatalf("probe message = %q", outcome.Message)
		}
		got := mustGetApp(t, f, id)
		if !got.Lapsed || got.ID != id {
			t.Fatalf("after refusal = %+v", got)
		}
	}
	if got := mustGetApp(t, f, granted.ID); got.Connection != entity.ConnectionPlatformAuthorization || got.PlatformID != platform.ID || got.Suspended || !sameCaps(got.Capabilities, []string{entity.CapabilityMiniProgramLogin, entity.CapabilitySubscribeMessage}) {
		t.Fatalf("lapse rewrote the grant: %+v", got)
	}
	if got := mustGetApp(t, f, held.App.ID); got.Connection != entity.ConnectionDirectHold || got.PlatformID != 0 || got.Suspended {
		t.Fatalf("lapse rewrote the direct hold: %+v", got)
	}
	if got := mustGetApp(t, f, granted.ID); got.UpdateBy != 0 {
		t.Fatalf("lapse update_by = %d, want the system", got.UpdateBy)
	}
	if got := mustGetApp(t, f, held.App.ID); got.UpdateBy != 0 {
		t.Fatalf("direct-hold lapse update_by = %d, want the system", got.UpdateBy)
	}
	probes := len(f.api.appProbes)
	again, err := f.apps.Probe(context.Background(), granted.ID)
	if !errors.Is(err, wechat.ErrAppLapsed) || again != nil {
		t.Fatalf("probe while lapsed = (%v, %v)", again, err)
	}
	if len(f.api.appProbes) != probes {
		t.Fatal("a lapsed app was sent to WeChat")
	}
}

func TestAppProbeBusinessRejectionAndTimeoutDoNotLapse(t *testing.T) {
	classes := []CallClass{CallBusinessRejection, CallUnavailable}
	for _, class := range classes {
		f := newAuthFixture(t)
		held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
		if err != nil {
			t.Fatalf("direct hold: %v", err)
		}
		if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: held.App.ID, Suspended: true, ActorID: 8}); err != nil {
			t.Fatalf("suspend: %v", err)
		}
		before := describeApp(mustGetApp(t, f, held.App.ID))
		f.api.appProbe = ProbeAppResult{Class: class, Message: "这次没有受理"}
		outcome, err := f.apps.Probe(context.Background(), held.App.ID)
		if err != nil || outcome == nil || outcome.OK {
			t.Fatalf("class %d probe = (%+v, %v)", class, outcome, err)
		}
		got := mustGetApp(t, f, held.App.ID)
		if got.Lapsed || describeApp(got) != before {
			t.Fatalf("class %d changed the app\n got %s\nwant %s", class, describeApp(got), before)
		}
	}
}

func TestLapsedAndWithdrawnAppsRefuseProbe(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "密钥被拒绝"}
	if _, err := f.apps.Probe(context.Background(), held.App.ID); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallSuccess, Message: "连接成功"}
	outcome, err := f.apps.Probe(context.Background(), held.App.ID)
	if !errors.Is(err, wechat.ErrAppLapsed) || outcome != nil {
		t.Fatalf("lapsed probe = (%v, %v)", outcome, err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: held.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	outcome, err = f.apps.Probe(context.Background(), held.App.ID)
	if !errors.Is(err, wechat.ErrAppNotFound) || outcome != nil {
		t.Fatalf("withdrawn probe = (%v, %v)", outcome, err)
	}
	if len(f.api.appProbes) != 1 {
		t.Fatalf("probes = %d, want only the refusal that lapsed the app", len(f.api.appProbes))
	}
}

func TestReplacementAppSecretClearsLapseOnTheSameDirectHold(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "密钥被拒绝"}
	if _, err := f.apps.Probe(context.Background(), held.App.ID); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	renamed, err := f.apps.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      held.App.ID,
		Name:    "只改名称",
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !renamed.Lapsed || renamed.ID != held.App.ID {
		t.Fatalf("rename = %+v", renamed)
	}
	revealed, err := f.apps.RegeneratePushSecret(context.Background(), RegeneratePushSecretRequest{ID: held.App.ID, ActorID: 9})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if !revealed.App.Lapsed || revealed.App.ID != held.App.ID {
		t.Fatalf("regenerate = %+v", revealed.App)
	}
	restored, err := f.apps.Update(context.Background(), UpdateDirectHoldRequest{
		ID:        held.App.ID,
		Name:      "换了密钥",
		AppSecret: "app-secret-value-NEXT",
		ActorID:   9,
	})
	if err != nil {
		t.Fatalf("replace secret: %v", err)
	}
	if restored.Lapsed || restored.ID != held.App.ID || restored.Connection != entity.ConnectionDirectHold || restored.AppID != held.App.AppID {
		t.Fatalf("restored = %+v", restored)
	}
	if restored.UpdateBy != 9 {
		t.Fatalf("restore update_by = %d, want the operator", restored.UpdateBy)
	}
	f.api.appProbe = ProbeAppResult{Class: CallSuccess, Message: "连接成功"}
	outcome, err := f.apps.Probe(context.Background(), held.App.ID)
	if err != nil || outcome == nil || !outcome.OK {
		t.Fatalf("probe after restore = (%+v, %v)", outcome, err)
	}
	if len(f.api.appProbes) != 2 || f.api.appProbes[1].AppSecret != "app-secret-value-NEXT" {
		t.Fatalf("probe after restore did not use the new secret: %+v", f.api.appProbes)
	}
	if mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("probe after restore lapsed the app again")
	}
}

func TestReauthorizationClearsLapseOnTheSamePlatform(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	if _, err := f.apps.Update(context.Background(), UpdateDirectHoldRequest{ID: granted.ID, Name: "运营改过的名字", ActorID: 9}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: granted.ID, Suspended: true, ActorID: 9}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "授权已被取消"}
	if _, err := f.apps.Probe(context.Background(), granted.ID); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	f.script(AuthorizationGrant{
		AppID:        granted.AppID,
		Name:         "微信现在的名字",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-AGAIN",
	})
	again, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-restore",
	})
	if err != nil {
		t.Fatalf("reauthorize: %v", err)
	}
	if again.ID != granted.ID || again.PlatformID != platform.ID || again.Connection != entity.ConnectionPlatformAuthorization || again.Lapsed || !again.Suspended || again.Name != "运营改过的名字" {
		t.Fatalf("restored = %+v", again)
	}
	if !sameCaps(again.Capabilities, []string{entity.CapabilitySubscribeMessage}) {
		t.Fatalf("capabilities = %#v", again.Capabilities)
	}
	f.api.appProbe = ProbeAppResult{Class: CallSuccess, Message: "连接成功"}
	outcome, err := f.apps.Probe(context.Background(), granted.ID)
	if err != nil || outcome == nil || !outcome.OK {
		t.Fatalf("probe after reauthorization = (%+v, %v)", outcome, err)
	}
}

func TestUnreadyPlatformFailsAppProbeWithoutLapsingOthers(t *testing.T) {
	f := newAuthFixture(t)
	east := f.holdReady(t, "华东平台", "wxcompappid000001")
	south := f.holdReady(t, "华南平台", "wxcompappid000002")
	southApp := f.grantMini(t, south.ID, "wxminiappid000088", []string{entity.CapabilityMiniProgramLogin})
	eastApp := f.grantMini(t, east.ID, "wxminiappid000099", []string{entity.CapabilitySubscribeMessage})
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), south.ID); err != nil {
		t.Fatalf("release ticket: %v", err)
	}
	southBefore := describeApp(mustGetApp(t, f, southApp.ID))
	outcome, err := f.apps.Probe(context.Background(), southApp.ID)
	if err != nil || outcome == nil || outcome.OK {
		t.Fatalf("unready probe = (%+v, %v)", outcome, err)
	}
	if got := describeApp(mustGetApp(t, f, southApp.ID)); got != southBefore || mustGetApp(t, f, southApp.ID).Lapsed {
		t.Fatalf("unready probe changed the app\n got %s\nwant %s", got, southBefore)
	}
	if len(f.api.appProbes) != 0 {
		t.Fatalf("unready platform still called WeChat: %+v", f.api.appProbes)
	}
	for _, id := range []int64{eastApp.ID, held.App.ID} {
		outcome, err = f.apps.Probe(context.Background(), id)
		if err != nil || outcome == nil || !outcome.OK {
			t.Fatalf("probe %d while another platform is unready = (%+v, %v)", id, outcome, err)
		}
	}
	if mustGetApp(t, f, southApp.ID).Lapsed || mustGetApp(t, f, eastApp.ID).Lapsed || mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("an unready platform lapsed an app")
	}
	if len(f.api.appProbes) != 2 {
		t.Fatalf("probes = %d, want the other platform's app and the direct hold", len(f.api.appProbes))
	}
}

func mustGetApp(t *testing.T, f *authFixture, id int64) *entity.App {
	t.Helper()
	got, err := f.apps.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %d: %v", id, err)
	}
	return got
}
