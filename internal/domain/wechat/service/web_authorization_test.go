package service

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/listq"
)

func TestServiceAccountWebAuthorizationDefaultsToOpenID(t *testing.T) {
	svc := newTestAppService(t)
	reveal, err := svc.Create(context.Background(), serviceHold("wxservice00000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if reveal.App.WebAuthScope != entity.WebAuthScopeOpenID {
		t.Fatalf("scope = %q, want openid only", reveal.App.WebAuthScope)
	}
	if reveal.App.ReturnURL != "" {
		t.Fatalf("return URL = %q, want none", reveal.App.ReturnURL)
	}
	got, err := svc.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WebAuthScope != entity.WebAuthScopeOpenID || got.ReturnURL != "" {
		t.Fatalf("stored scope = %q return URL = %q", got.WebAuthScope, got.ReturnURL)
	}
}

func TestOperatorSwitchesWebAuthorizationScope(t *testing.T) {
	svc := newTestAppService(t)
	reveal, err := svc.Create(context.Background(), serviceHold("wxservice00000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	profile, err := svc.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:      reveal.App.ID,
		Scope:   entity.WebAuthScopeProfile,
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("switch to profile: %v", err)
	}
	if profile.WebAuthScope != entity.WebAuthScopeProfile || profile.UpdateBy != 9 {
		t.Fatalf("profile = scope %q update_by %d", profile.WebAuthScope, profile.UpdateBy)
	}
	if profile.ReturnURL != "" || profile.CreateBy != 7 {
		t.Fatalf("switch rewrote the app: %+v", profile)
	}
	stored, err := svc.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.WebAuthScope != entity.WebAuthScopeProfile || stored.UpdateBy != 9 {
		t.Fatalf("stored = scope %q update_by %d", stored.WebAuthScope, stored.UpdateBy)
	}
	back, err := svc.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:      reveal.App.ID,
		Scope:   entity.WebAuthScopeOpenID,
		ActorID: 4,
	})
	if err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if back.WebAuthScope != entity.WebAuthScopeOpenID || back.UpdateBy != 4 {
		t.Fatalf("openid = scope %q update_by %d", back.WebAuthScope, back.UpdateBy)
	}
}

func TestOperatorSavesReturnURLOnTheServiceAccount(t *testing.T) {
	svc := newTestAppService(t)
	reveal, err := svc.Create(context.Background(), serviceHold("wxservice00000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	saved, err := svc.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:        reveal.App.ID,
		ReturnURL: "  https://h5.example/back  ",
		Scope:     entity.WebAuthScopeOpenID,
		ActorID:   9,
	})
	if err != nil {
		t.Fatalf("save return URL: %v", err)
	}
	if saved.ReturnURL != "https://h5.example/back" || saved.WebAuthScope != entity.WebAuthScopeOpenID || saved.UpdateBy != 9 {
		t.Fatalf("saved = %+v", saved)
	}
	got, err := svc.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReturnURL != "https://h5.example/back" {
		t.Fatalf("stored return URL = %q", got.ReturnURL)
	}

	subscription := serviceHold("wxofficial000002", 7)
	subscription.Name = "订阅号乙"
	subscription.Kind = entity.AccountKindSubscription
	sub, err := svc.Create(context.Background(), subscription)
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	if _, err := svc.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:        sub.App.ID,
		ReturnURL: "https://h5.example/back",
		Scope:     entity.WebAuthScopeOpenID,
		ActorID:   9,
	}); !errors.Is(err, wechat.ErrWebAuthorizationRefused) {
		t.Fatalf("subscription configure err = %v", err)
	}
	mini, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("mini: %v", err)
	}
	if _, err := svc.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:        mini.App.ID,
		ReturnURL: "https://h5.example/back",
		Scope:     entity.WebAuthScopeProfile,
		ActorID:   9,
	}); !errors.Is(err, wechat.ErrWebAuthorizationRefused) {
		t.Fatalf("mini program configure err = %v", err)
	}
}

func TestGrantedServiceAccountDefaultsToOpenID(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000099",
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityWebAuthorization},
		RefreshToken: "authorizer-refresh-SSSS",
	})
	service, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-service",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if service.WebAuthScope != entity.WebAuthScopeOpenID || service.ReturnURL != "" {
		t.Fatalf("granted scope = %q return URL = %q", service.WebAuthScope, service.ReturnURL)
	}
	saved, err := f.apps.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:        service.ID,
		ReturnURL: "https://h5.example/granted",
		Scope:     entity.WebAuthScopeProfile,
		ActorID:   8,
	})
	if err != nil {
		t.Fatalf("configure granted: %v", err)
	}
	if saved.ReturnURL != "https://h5.example/granted" || saved.WebAuthScope != entity.WebAuthScopeProfile || saved.UpdateBy != 8 {
		t.Fatalf("configured grant = %+v", saved)
	}
	if saved.Connection != entity.ConnectionPlatformAuthorization || saved.PlatformID != platform.ID {
		t.Fatalf("configure rewrote the connection: %+v", saved)
	}
}

func TestWebAuthorizationCannotStartWithoutReturnURL(t *testing.T) {
	svc := newTestAppService(t)
	reveal, err := svc.Create(context.Background(), serviceHold("wxservice00000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	start, err := svc.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{
		ID:        reveal.App.ID,
		ReturnURL: "https://evil.example/steal",
	})
	if !errors.Is(err, wechat.ErrWebAuthorizationRefused) || start != nil {
		t.Fatalf("start without a stored return URL = (%v, %v)", start, err)
	}
}

func TestWebAuthorizationCannotStartWhenTheAppCannotRunIt(t *testing.T) {
	f := newAuthFixture(t)
	subscription := serviceHold("wxofficial000002", 7)
	subscription.Name = "订阅号乙"
	subscription.Kind = entity.AccountKindSubscription
	sub, err := f.apps.Create(context.Background(), subscription)
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	mini, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("mini: %v", err)
	}
	for _, id := range []int64{sub.App.ID, mini.App.ID} {
		start, err := f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{ID: id})
		if !errors.Is(err, wechat.ErrWebAuthorizationRefused) || start != nil {
			t.Fatalf("start %d = (%v, %v)", id, start, err)
		}
	}

	held, err := f.apps.Create(context.Background(), serviceHold("wxservice00000001", 7))
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if _, err := f.apps.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID: held.App.ID, ReturnURL: "https://h5.example/back", Scope: entity.WebAuthScopeOpenID, ActorID: 9,
	}); err != nil {
		t.Fatalf("return URL: %v", err)
	}
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: held.App.ID, Suspended: true, ActorID: 9}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	start, err := f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{
		ID: held.App.ID, ReturnURL: "https://evil.example/steal",
	})
	if !errors.Is(err, wechat.ErrWebAuthorizationRefused) || start != nil {
		t.Fatalf("suspended start = (%v, %v)", start, err)
	}

	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: held.App.ID, Suspended: false, ActorID: 9}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "密钥被拒绝"}
	if _, err := f.apps.Probe(context.Background(), held.App.ID); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	start, err = f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{ID: held.App.ID})
	if !errors.Is(err, wechat.ErrAppLapsed) || start != nil {
		t.Fatalf("lapsed start = (%v, %v)", start, err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: held.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	start, err = f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{ID: held.App.ID})
	if !errors.Is(err, wechat.ErrAppNotFound) || start != nil {
		t.Fatalf("withdrawn start = (%v, %v)", start, err)
	}

	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000099",
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityCustomMenu},
		RefreshToken: "authorizer-refresh-SSSS",
	})
	granted, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-service",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := f.apps.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID: granted.ID, ReturnURL: "https://h5.example/granted", Scope: entity.WebAuthScopeOpenID, ActorID: 8,
	}); err != nil {
		t.Fatalf("configure grant: %v", err)
	}
	start, err = f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{ID: granted.ID})
	if !errors.Is(err, wechat.ErrWebAuthorizationRefused) || start != nil {
		t.Fatalf("missing capability start = (%v, %v)", start, err)
	}

	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000099",
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityWebAuthorization},
		RefreshToken: "authorizer-refresh-SSSS",
	})
	restored, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-again",
	})
	if err != nil {
		t.Fatalf("regrant: %v", err)
	}
	if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), platform.ID); err != nil {
		t.Fatalf("release ticket: %v", err)
	}
	start, err = f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{ID: restored.ID})
	if !errors.Is(err, wechat.ErrPlatformNotReady) || start != nil {
		t.Fatalf("unready platform start = (%v, %v)", start, err)
	}
}

func TestBrowserLandsOnlyOnTheStoredReturnURL(t *testing.T) {
	f := newAuthFixture(t)
	app := f.readyWebService(t, "https://h5.example/back", entity.WebAuthScopeOpenID)
	start, err := f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{
		ID:        app.ID,
		ReturnURL: "https://evil.example/steal",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authorize, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorize URL %q: %v", start.AuthorizeURL, err)
	}
	query := authorize.Query()
	wantRedirect := testCallbackBase + "/wechat/app/" + strconv.FormatInt(app.ID, 10) + "/web-authorization/return"
	if query.Get("appid") != app.AppID || query.Get("scope") != "snsapi_base" || query.Get("redirect_uri") != wantRedirect {
		t.Fatalf("authorize query = %v", query)
	}
	if query.Get("component_appid") != "" {
		t.Fatalf("direct hold authorize URL names a component: %q", start.AuthorizeURL)
	}
	if strings.Contains(start.AuthorizeURL, "evil.example") || strings.Contains(start.AuthorizeURL, "h5.example") {
		t.Fatalf("authorize URL carried a return address: %q", start.AuthorizeURL)
	}

	f.api.webGrant = &WebAuthorizationExchange{
		Class:     CallSuccess,
		OpenID:    "o_wxservice_fan",
		UnionID:   "u_wxservice",
		Nickname:  "微信用户",
		AvatarURL: "https://cdn.example/a.png",
	}
	landing, err := f.apps.AcceptWebAuthorization(context.Background(), AcceptWebAuthorizationRequest{
		ID:        app.ID,
		Code:      "wx-code-1",
		ReturnURL: "https://evil.example/steal",
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	landed, err := url.Parse(landing.Location)
	if err != nil {
		t.Fatalf("location %q: %v", landing.Location, err)
	}
	if landed.Scheme != "https" || landed.Host != "h5.example" || landed.Path != "/back" {
		t.Fatalf("landed at %q", landing.Location)
	}
	if landed.Query().Get("handoff_code") == "" || strings.Contains(landing.Location, "evil.example") || strings.Contains(landing.Location, "wx-code-1") {
		t.Fatalf("location = %q", landing.Location)
	}
	if len(f.api.webExchanges) != 1 || f.api.webExchanges[0].Code != "wx-code-1" || f.api.webExchanges[0].Profile {
		t.Fatalf("exchange = %+v", f.api.webExchanges)
	}
}

func TestRedeemHandoffCodeOnceForOpenID(t *testing.T) {
	f := newAuthFixture(t)
	app := f.readyWebService(t, "https://h5.example/back?from=wechat", entity.WebAuthScopeOpenID)
	before, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	f.api.webGrant = &WebAuthorizationExchange{
		Class:     CallSuccess,
		OpenID:    "o_wxservice_fan",
		UnionID:   "u_wxservice",
		Nickname:  "微信用户",
		AvatarURL: "https://cdn.example/a.png",
	}
	landing, err := f.apps.AcceptWebAuthorization(context.Background(), AcceptWebAuthorizationRequest{
		ID:   app.ID,
		Code: "wx-code-openid",
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	landed, err := url.Parse(landing.Location)
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	if landed.Query().Get("from") != "wechat" {
		t.Fatalf("landing dropped the stored query: %q", landing.Location)
	}
	code := landed.Query().Get("handoff_code")
	identity, err := f.apps.RedeemHandoffCode(context.Background(), RedeemHandoffCodeRequest{Code: code})
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if identity.OpenID != "o_wxservice_fan" || identity.UnionID != "u_wxservice" || identity.Nickname != "" || identity.AvatarURL != "" {
		t.Fatalf("identity = %+v", identity)
	}
	after, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != len(before) || len(after) != 1 || after[0].ID != app.ID {
		t.Fatalf("redeem created a row: before %d after %+v", len(before), after)
	}
}

func TestRedeemIncludesNicknameAndAvatarOnlyWhenAsked(t *testing.T) {
	f := newAuthFixture(t)
	app := f.readyWebService(t, "https://h5.example/profile", entity.WebAuthScopeProfile)
	start, err := f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{
		ID:        app.ID,
		ReturnURL: "https://evil.example/steal",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	authorize, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorize URL: %v", err)
	}
	if authorize.Query().Get("scope") != "snsapi_userinfo" || strings.Contains(start.AuthorizeURL, "evil.example") {
		t.Fatalf("authorize URL = %q", start.AuthorizeURL)
	}
	f.api.webGrant = &WebAuthorizationExchange{
		Class:     CallSuccess,
		OpenID:    "o_profile_fan",
		UnionID:   "u_profile",
		Nickname:  "微信昵称",
		AvatarURL: "https://cdn.example/face.png",
	}
	landing, err := f.apps.AcceptWebAuthorization(context.Background(), AcceptWebAuthorizationRequest{
		ID:        app.ID,
		Code:      "wx-code-profile",
		ReturnURL: "https://evil.example/steal",
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(f.api.webExchanges) != 1 || !f.api.webExchanges[0].Profile {
		t.Fatalf("exchange = %+v", f.api.webExchanges)
	}
	code := mustHandoffCode(t, landing.Location)
	identity, err := f.apps.RedeemHandoffCode(context.Background(), RedeemHandoffCodeRequest{Code: code})
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if identity.OpenID != "o_profile_fan" || identity.UnionID != "u_profile" || identity.Nickname != "微信昵称" || identity.AvatarURL != "https://cdn.example/face.png" {
		t.Fatalf("identity = %+v", identity)
	}
	if strings.Contains(landing.Location, "evil.example") {
		t.Fatalf("profile landing used the request URL: %q", landing.Location)
	}
}

func TestHandoffCodeCannotBeReplayedOrGuessed(t *testing.T) {
	f := newAuthFixture(t)
	app := f.readyWebService(t, "https://h5.example/back", entity.WebAuthScopeOpenID)
	f.api.webGrant = &WebAuthorizationExchange{Class: CallSuccess, OpenID: "o_once", UnionID: "u_once"}
	landing, err := f.apps.AcceptWebAuthorization(context.Background(), AcceptWebAuthorizationRequest{
		ID:   app.ID,
		Code: "wx-code-once",
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	code := mustHandoffCode(t, landing.Location)
	if _, err := f.apps.RedeemHandoffCode(context.Background(), RedeemHandoffCodeRequest{Code: "never-issued"}); !errors.Is(err, wechat.ErrHandoffCodeInvalid) {
		t.Fatalf("unknown code err = %v", err)
	}
	first, err := f.apps.RedeemHandoffCode(context.Background(), RedeemHandoffCodeRequest{Code: code})
	if err != nil || first.OpenID != "o_once" || first.UnionID != "u_once" {
		t.Fatalf("first redeem = (%+v, %v)", first, err)
	}
	second, err := f.apps.RedeemHandoffCode(context.Background(), RedeemHandoffCodeRequest{Code: code})
	if !errors.Is(err, wechat.ErrHandoffCodeInvalid) || second != nil {
		t.Fatalf("second redeem = (%v, %v)", second, err)
	}
}

func TestPlatformWebAuthorizationUsesTheStoredReturnURL(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000099",
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityWebAuthorization},
		RefreshToken: "authorizer-refresh-SSSS",
	})
	granted, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-service",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := f.apps.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID: granted.ID, ReturnURL: "https://h5.example/platform", Scope: entity.WebAuthScopeOpenID, ActorID: 8,
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	start, err := f.apps.StartWebAuthorization(context.Background(), StartWebAuthorizationRequest{
		ID: granted.ID, ReturnURL: "https://evil.example/steal",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	query := mustURL(t, start.AuthorizeURL).Query()
	if query.Get("component_appid") != platform.ComponentAppID || query.Get("appid") != granted.AppID || strings.Contains(start.AuthorizeURL, "evil.example") {
		t.Fatalf("authorize URL = %q", start.AuthorizeURL)
	}
	f.api.webGrant = &WebAuthorizationExchange{Class: CallSuccess, OpenID: "o_platform", UnionID: "u_platform", Nickname: "不该出现"}
	landing, err := f.apps.AcceptWebAuthorization(context.Background(), AcceptWebAuthorizationRequest{
		ID: granted.ID, Code: "wx-code-platform", ReturnURL: "https://evil.example/steal",
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	landed := mustURL(t, landing.Location)
	if landed.Host != "h5.example" || landed.Path != "/platform" || strings.Contains(landing.Location, "evil.example") {
		t.Fatalf("location = %q", landing.Location)
	}
	if len(f.api.webExchanges) != 1 || f.api.webExchanges[0].ComponentAppID != platform.ComponentAppID || f.api.webExchanges[0].AppSecret != "" || f.api.webExchanges[0].Profile {
		t.Fatalf("exchange = %+v", f.api.webExchanges)
	}
	identity, err := f.apps.RedeemHandoffCode(context.Background(), RedeemHandoffCodeRequest{Code: landed.Query().Get("handoff_code")})
	if err != nil || identity.OpenID != "o_platform" || identity.UnionID != "u_platform" || identity.Nickname != "" {
		t.Fatalf("identity = (%+v, %v)", identity, err)
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("apps after redeem = (%d, %v)", len(listed), err)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url %q: %v", raw, err)
	}
	return parsed
}

func mustHandoffCode(t *testing.T, location string) string {
	t.Helper()
	landed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("location %q: %v", location, err)
	}
	code := landed.Query().Get("handoff_code")
	if code == "" {
		t.Fatalf("location %q has no handoff code", location)
	}
	return code
}

// memHandoffRepo is the in-memory single-use Handoff Code store. It has
// no lifetime. Consume deletes the grant.
type memHandoffRepo struct {
	next int
	rows map[string]repository.HandoffGrant
}

func newMemHandoffRepo() *memHandoffRepo {
	return &memHandoffRepo{rows: map[string]repository.HandoffGrant{}}
}

func (r *memHandoffRepo) Issue(_ context.Context, grant repository.HandoffGrant) (string, error) {
	r.next++
	code := "hc-" + strconv.Itoa(r.next)
	r.rows[code] = grant
	return code, nil
}

func (r *memHandoffRepo) Consume(_ context.Context, code string) (repository.HandoffGrant, error) {
	grant, ok := r.rows[code]
	if !ok {
		return repository.HandoffGrant{}, wechat.ErrHandoffCodeInvalid
	}
	delete(r.rows, code)
	return grant, nil
}

func (f *authFixture) readyWebService(t *testing.T, returnURL, scope string) *entity.App {
	t.Helper()
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice00000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	saved, err := f.apps.ConfigureWebAuthorization(context.Background(), ConfigureWebAuthorizationRequest{
		ID:        reveal.App.ID,
		ReturnURL: returnURL,
		Scope:     scope,
		ActorID:   9,
	})
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	return saved
}

func serviceHold(appID string, actorID int64) CreateDirectHoldRequest {
	return CreateDirectHoldRequest{
		Name:      "服务号甲",
		AppID:     appID,
		AppType:   entity.AppTypeOfficialAccount,
		Kind:      entity.AccountKindService,
		AppSecret: testAppSecret,
		ActorID:   actorID,
	}
}
