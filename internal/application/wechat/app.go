package wechat

import (
	"context"

	wechatv1 "glex/api/wechat/v1"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertApp maps an app to the proto DTO. Only masks cross the wire —
// the AppSecret, the Push Secret, and the ciphertext stay server-side
// (ADR 0029).
func convertApp(app *entity.App) *wechatv1.WechatApp {
	return &wechatv1.WechatApp{
		AppId:               app.ID,
		Name:                app.Name,
		WxAppId:             app.AppID,
		AppType:             appTypeProto(app.AppType),
		AccountKind:         accountKindProto(app.Kind),
		Connection:          connectionProto(app.Connection),
		PlatformId:          app.PlatformID,
		GrantedCapabilities: capabilitiesProto(app.Capabilities),
		AppSecretMask:       app.AppSecretMask,
		TokenMask:           app.TokenMask,
		EncodingAesKeyMask:  app.EncodingAESKeyMask,
		CallbackAddress:     app.CallbackAddress,
		Suspended:           app.Suspended,
		Lapsed:              app.Lapsed,
		ReturnUrl:           app.ReturnURL,
		WebAuthScope:        webAuthScopeProto(app.WebAuthScope),
		KeyVersion:          int32(app.KeyVersion),
		CreateBy:            app.CreateBy,
		UpdateBy:            app.UpdateBy,
		CreatedAt:           timestamppb.New(app.CreatedAt),
		UpdatedAt:           timestamppb.New(app.UpdatedAt),
	}
}

func convertReveal(reveal *wechatservice.PushSecretReveal) *wechatv1.WechatAppPushSecret {
	return &wechatv1.WechatAppPushSecret{
		App:            convertApp(reveal.App),
		Token:          reveal.Token,
		EncodingAesKey: reveal.EncodingAESKey,
	}
}

func appTypeProto(v string) wechatv1.WechatAppType {
	switch v {
	case entity.AppTypeMiniProgram:
		return wechatv1.WechatAppType_WECHAT_APP_TYPE_MINI_PROGRAM
	case entity.AppTypeOfficialAccount:
		return wechatv1.WechatAppType_WECHAT_APP_TYPE_OFFICIAL_ACCOUNT
	default:
		return wechatv1.WechatAppType_WECHAT_APP_TYPE_UNSPECIFIED
	}
}

func appTypeDomain(v wechatv1.WechatAppType) string {
	switch v {
	case wechatv1.WechatAppType_WECHAT_APP_TYPE_MINI_PROGRAM:
		return entity.AppTypeMiniProgram
	case wechatv1.WechatAppType_WECHAT_APP_TYPE_OFFICIAL_ACCOUNT:
		return entity.AppTypeOfficialAccount
	default:
		return ""
	}
}

func accountKindProto(v string) wechatv1.WechatAccountKind {
	switch v {
	case entity.AccountKindService:
		return wechatv1.WechatAccountKind_WECHAT_ACCOUNT_KIND_SERVICE
	case entity.AccountKindSubscription:
		return wechatv1.WechatAccountKind_WECHAT_ACCOUNT_KIND_SUBSCRIPTION
	default:
		return wechatv1.WechatAccountKind_WECHAT_ACCOUNT_KIND_UNSPECIFIED
	}
}

func accountKindDomain(v wechatv1.WechatAccountKind) string {
	switch v {
	case wechatv1.WechatAccountKind_WECHAT_ACCOUNT_KIND_SERVICE:
		return entity.AccountKindService
	case wechatv1.WechatAccountKind_WECHAT_ACCOUNT_KIND_SUBSCRIPTION:
		return entity.AccountKindSubscription
	default:
		return ""
	}
}

func connectionProto(v string) wechatv1.WechatConnection {
	switch v {
	case entity.ConnectionDirectHold:
		return wechatv1.WechatConnection_WECHAT_CONNECTION_DIRECT_HOLD
	case entity.ConnectionPlatformAuthorization:
		return wechatv1.WechatConnection_WECHAT_CONNECTION_PLATFORM_AUTHORIZATION
	default:
		return wechatv1.WechatConnection_WECHAT_CONNECTION_UNSPECIFIED
	}
}

// appListSpec is this list's filterable idents and default order
// (ADR 0024). The idents are the stored column names.
var appListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("name", filtering.TypeString),
		filtering.DeclareIdent("wx_app_id", filtering.TypeString),
		filtering.DeclareIdent("app_type", filtering.TypeString),
		filtering.DeclareIdent("account_kind", filtering.TypeString),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// CreateWechatApp connects an app by Direct Hold. create_by and update_by
// are stamped from the authenticated Principal (ADR 0002). The response
// returns the generated Push Secret in full once.
func (s *WechatApplication) CreateWechatApp(ctx context.Context, req *wechatv1.CreateWechatAppRequest) (*wechatv1.WechatAppPushSecret, error) {
	created, err := s.apps.Create(ctx, wechatservice.CreateDirectHoldRequest{
		Name:      req.GetName(),
		AppID:     req.GetWxAppId(),
		AppType:   appTypeDomain(req.GetAppType()),
		Kind:      accountKindDomain(req.GetAccountKind()),
		AppSecret: req.GetAppSecret(),
		ActorID:   s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.create_app", err)
		return nil, err
	}
	return convertReveal(created), nil
}

// ListWechatApps lists apps that are not withdrawn. Responses carry masks.
func (s *WechatApplication) ListWechatApps(ctx context.Context, req *wechatv1.ListWechatAppsRequest) (*wechatv1.WechatAppSet, error) {
	q, page, err := listq.Parse(req, appListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_apps", err)
		return nil, err
	}
	apps, err := s.apps.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_apps", err)
		return nil, err
	}
	set := &wechatv1.WechatAppSet{Apps: make([]*wechatv1.WechatApp, 0, len(apps))}
	for _, app := range apps {
		set.Apps = append(set.Apps, convertApp(app))
	}
	set.NextPageToken = page.NextToken(len(apps))
	return set, nil
}

// GetWechatApp retrieves one app that is not withdrawn.
func (s *WechatApplication) GetWechatApp(ctx context.Context, req *wechatv1.GetWechatAppRequest) (*wechatv1.WechatApp, error) {
	app, err := s.apps.Get(ctx, req.GetAppId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.get_app", err)
		return nil, err
	}
	return convertApp(app), nil
}

// UpdateWechatApp corrects the display name and replaces the AppSecret
// when one was submitted. The response carries masks only.
func (s *WechatApplication) UpdateWechatApp(ctx context.Context, req *wechatv1.UpdateWechatAppRequest) (*wechatv1.WechatApp, error) {
	updated, err := s.apps.Update(ctx, wechatservice.UpdateDirectHoldRequest{
		ID:        req.GetAppId(),
		Name:      req.GetName(),
		AppID:     req.GetWxAppId(),
		AppSecret: req.GetAppSecret(),
		ActorID:   s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.update_app", err)
		return nil, err
	}
	return convertApp(updated), nil
}

// SetWechatAppSuspended suspends or resumes the app. The row stays live.
func (s *WechatApplication) SetWechatAppSuspended(ctx context.Context, req *wechatv1.SetWechatAppSuspendedRequest) (*wechatv1.WechatApp, error) {
	updated, err := s.apps.SetSuspended(ctx, wechatservice.SetSuspendedRequest{
		ID:        req.GetAppId(),
		Suspended: req.GetSuspended(),
		ActorID:   s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.set_app_suspended", err)
		return nil, err
	}
	return convertApp(updated), nil
}

// RegenerateWechatAppPushSecret returns the new Push Secret in full once.
func (s *WechatApplication) RegenerateWechatAppPushSecret(ctx context.Context, req *wechatv1.RegenerateWechatAppPushSecretRequest) (*wechatv1.WechatAppPushSecret, error) {
	reveal, err := s.apps.RegeneratePushSecret(ctx, wechatservice.RegeneratePushSecretRequest{
		ID:      req.GetAppId(),
		ActorID: s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.regenerate_app_push_secret", err)
		return nil, err
	}
	return convertReveal(reveal), nil
}

// ProbeWechatApp runs the WeChat Connectivity Test. A failed test is
// the outcome; a lapsed or missing app is an error.
func (s *WechatApplication) ProbeWechatApp(ctx context.Context, req *wechatv1.ProbeWechatAppRequest) (*wechatv1.ProbeWechatAppResult, error) {
	outcome, err := s.apps.Probe(ctx, req.GetAppId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.probe_app", err)
		return nil, err
	}
	return &wechatv1.ProbeWechatAppResult{
		Ok:      outcome.OK,
		Message: outcome.Message,
	}, nil
}

// WithdrawWechatApp soft-deletes the app.
func (s *WechatApplication) WithdrawWechatApp(ctx context.Context, req *wechatv1.WithdrawWechatAppRequest) (*emptypb.Empty, error) {
	if err := s.apps.Withdraw(ctx, wechatservice.WithdrawAppRequest{
		ID:      req.GetAppId(),
		ActorID: s.actor(ctx),
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.withdraw_app", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
