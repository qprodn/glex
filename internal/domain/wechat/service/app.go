package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// CreateDirectHoldRequest is one Direct Hold: the display name, the
// AppID, the AppSecret the operator typed, the app type, and — for an
// official account — the Service Account or Subscription Account kind.
// A mini program carries no kind. The actor is stamped into the audit
// fields.
type CreateDirectHoldRequest struct {
	Name      string
	AppID     string
	AppType   string
	Kind      string
	AppSecret string
	ActorID   int64
}

// UpdateDirectHoldRequest corrects the display name and, when AppSecret
// is non-blank, replaces it. A non-empty AppID that differs from the
// stored value is refused. A blank AppSecret keeps the stored value and
// does not touch the ciphertext. The response never carries the secret.
type UpdateDirectHoldRequest struct {
	ID        int64
	Name      string
	AppID     string
	AppSecret string
	ActorID   int64
}

// RegeneratePushSecretRequest mints a new Token and EncodingAESKey for
// one live Direct Hold. The previous pair is replaced in the same write.
// Suspension and the connection are left as they are.
type RegeneratePushSecretRequest struct {
	ID      int64
	ActorID int64
}

// SetSuspendedRequest suspends or resumes one live app. Suspension is
// not withdrawal.
type SetSuspendedRequest struct {
	ID        int64
	Suspended bool
	ActorID   int64
}

// WithdrawAppRequest is one app withdrawal: the row and the actor
// stamped into the audit fields.
type WithdrawAppRequest struct {
	ID      int64
	ActorID int64
}

// SaveMenuRequest stores one Custom Menu on a Service Account. The
// actor is stamped into the audit fields. The call does not publish.
type SaveMenuRequest struct {
	ID      int64
	Menu    entity.CustomMenu
	ActorID int64
}

// PublishMenuRequest replaces the Service Account's default menu at
// WeChat with the menu already saved. The actor is stamped when WeChat
// accepts that menu.
type PublishMenuRequest struct {
	ID      int64
	ActorID int64
}

// PushSecretReveal is the one response that carries a generated Push
// Secret in full. App carries masks only. Get, List, and an AppSecret
// replacement never return this pair (ADR 0029).
type PushSecretReveal struct {
	App            *entity.App
	Token          string
	EncodingAESKey string
}

// StartAuthorizationRequest chooses the one ready platform a Platform
// Authorization will bind to. Starting does not create a WeChat App.
type StartAuthorizationRequest struct {
	PlatformID int64
}

// CompleteAuthorizationRequest is one grant WeChat has already issued:
// the platform it belongs to and the authorization code from that grant.
// The write is stamped as the system itself.
type CompleteAuthorizationRequest struct {
	PlatformID int64
	AuthCode   string
}

// App is the wechat app domain service: the only path to WeChat App rows
// (ADR 0005). Apps connect by Direct Hold or by Platform Authorization.
type App interface {
	Create(ctx context.Context, req CreateDirectHoldRequest) (*PushSecretReveal, error)
	Get(ctx context.Context, id int64) (*entity.App, error)
	List(ctx context.Context, q listq.Query) ([]*entity.App, error)
	Update(ctx context.Context, req UpdateDirectHoldRequest) (*entity.App, error)
	RegeneratePushSecret(ctx context.Context, req RegeneratePushSecretRequest) (*PushSecretReveal, error)
	SetSuspended(ctx context.Context, req SetSuspendedRequest) (*entity.App, error)
	Withdraw(ctx context.Context, req WithdrawAppRequest) error
	// StartAuthorization returns the link for one ready platform. An
	// unready or withdrawn platform is refused. No WeChat App is created.
	StartAuthorization(ctx context.Context, req StartAuthorizationRequest) (*AuthorizationLink, error)
	// CompleteAuthorization creates or restores the WeChat App WeChat
	// reported, bound to that platform. The actor is the system itself.
	CompleteAuthorization(ctx context.Context, req CompleteAuthorizationRequest) (*entity.App, error)
	// CountLiveAuthorizations counts apps not withdrawn that are
	// authorized through this platform.
	CountLiveAuthorizations(ctx context.Context, platformID int64) (int, error)
	// ConfigureWebAuthorization stores the return URL and the scope on a
	// Service Account. The actor is the Principal. Scope is openid only,
	// or openid plus nickname and avatar.
	ConfigureWebAuthorization(ctx context.Context, req ConfigureWebAuthorizationRequest) (*entity.App, error)
	// StartWebAuthorization begins a Web Authorization for one usable
	// Service Account. The request's return URL is ignored: the browser
	// is sent to WeChat, and later lands only on the URL stored on the app.
	StartWebAuthorization(ctx context.Context, req StartWebAuthorizationRequest) (*WebAuthorizationStart, error)
	// AcceptWebAuthorization runs after WeChat accepts the grant. The
	// browser is sent to the return URL stored on the app, with one
	// Handoff Code. The request's return URL is ignored.
	AcceptWebAuthorization(ctx context.Context, req AcceptWebAuthorizationRequest) (*WebAuthorizationLanding, error)
	// RedeemHandoffCode returns the identity once. A second redeem, or a
	// code this system did not issue, fails. It creates no WeChat User,
	// System User, or Login Session.
	RedeemHandoffCode(ctx context.Context, req RedeemHandoffCodeRequest) (*WebIdentity, error)
	// Probe runs the WeChat Connectivity Test for one live app. It
	// creates no WeChat Dispatch. A suspended app can be tested. A
	// lapsed app is refused until it is restored. A withdrawn app is
	// not found. Success leaves suspension, lapse, and connection as
	// they were. A definitive refusal lapses the app. A business
	// rejection, a timeout, or an unready platform fails the test and
	// leaves the app unlapsed.
	Probe(ctx context.Context, id int64) (*ProbeOutcome, error)
	// AdoptTemplate takes one public-library template onto a live app
	// through that app's current token. The same library template is
	// not adopted twice onto the same live app.
	AdoptTemplate(ctx context.Context, req AdoptTemplateRequest) (*entity.AdoptedTemplate, error)
	// ListTemplates returns the templates adopted onto this app,
	// including after the app is withdrawn.
	ListTemplates(ctx context.Context, appID int64) ([]*entity.AdoptedTemplate, error)
	// Send delivers one adopted template to one openid and waits until
	// WeChat accepts or rejects. Acceptance and business rejection each
	// record a new WeChat Dispatch. A timeout records no accepted
	// dispatch. A definitive refusal lapses the app and records no
	// accepted dispatch.
	Send(ctx context.Context, req SendDispatchRequest) (*entity.Dispatch, error)
	// ListDispatches returns attempts for live and withdrawn apps.
	// AppID 0 lists every attempt.
	ListDispatches(ctx context.Context, req ListDispatchesRequest) (*DispatchPage, error)
	// SaveMenu writes the Custom Menu document on one Service Account
	// and does not call WeChat. The last menu WeChat accepted is left
	// as it was.
	SaveMenu(ctx context.Context, req SaveMenuRequest) (*entity.App, error)
	// PublishMenu sends the whole saved menu and records it as the last
	// menu WeChat accepted. A later save does not change that record.
	PublishMenu(ctx context.Context, req PublishMenuRequest) (*entity.App, error)
	// MiniProgramLogin exchanges one mini program's one-time login code
	// for that app's openid and, when WeChat supplied one, its unionid.
	// Direct Hold and Platform Authorization share this entry. The call
	// takes no back-office Principal. It does not create a WeChat User,
	// a System User, or a Login Session, and session_key is not returned.
	MiniProgramLogin(ctx context.Context, req MiniProgramLoginRequest) (*MiniProgramIdentity, error)
}

// PlatformBindings is the slice of the app service the platform service
// needs: refuse a platform withdrawal while a live grant remains, and
// apply a grant WeChat pushed. The app service implements it.
type PlatformBindings interface {
	CountLiveAuthorizations(ctx context.Context, platformID int64) (int, error)
	CompleteAuthorization(ctx context.Context, req CompleteAuthorizationRequest) (*entity.App, error)
}

type appImpl struct {
	apps         repository.AppRepo
	platforms    repository.PlatformRepo
	templates    repository.TemplateRepo
	dispatches   repository.DispatchRepo
	handoffs     repository.HandoffCodeRepo
	openapi      OpenAPI
	callbackBase string
	logger       *slog.Logger
}

// NewAppService creates the app domain service. handoffs stores Handoff
// Codes. It is not a user repository.
func NewAppService(apps repository.AppRepo, platforms repository.PlatformRepo, templates repository.TemplateRepo, dispatches repository.DispatchRepo, handoffs repository.HandoffCodeRepo, openapi OpenAPI, callbackBase CallbackBase, logger *slog.Logger) App {
	return &appImpl{
		apps:         apps,
		platforms:    platforms,
		templates:    templates,
		dispatches:   dispatches,
		handoffs:     handoffs,
		openapi:      openapi,
		callbackBase: strings.TrimRight(string(callbackBase), "/"),
		logger:       logger.With(slog.String("component", "domain.wechat.app.svc")),
	}
}

func (s *appImpl) Create(ctx context.Context, req CreateDirectHoldRequest) (*PushSecretReveal, error) {
	if err := validateAppName(req.Name); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	if err := validateAppID(req.AppID); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	if err := validateDirectHoldShape(req.AppType, req.Kind); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	if err := validateAppSecret(req.AppSecret, true); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	if err := s.ensureAppIDAvailable(ctx, req.AppID); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	token, aesKey, err := generatePushSecret()
	if err != nil {
		err = wechat.ErrSecretCrypto
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	app := &entity.App{
		ID:                 s.apps.NextID(),
		Name:               req.Name,
		AppID:              req.AppID,
		AppType:            req.AppType,
		Kind:               req.Kind,
		Connection:         entity.ConnectionDirectHold,
		AppSecretMask:      maskSecret(req.AppSecret),
		TokenMask:          maskSecret(token),
		EncodingAESKeyMask: maskSecret(aesKey),
		WebAuthScope:       entity.WebAuthScopeOpenID,
		KeyVersion:         entity.KeyVersion,
		CreateBy:           req.ActorID,
		UpdateBy:           req.ActorID,
	}
	created, err := s.apps.Create(ctx, repository.SaveAppRequest{
		App: app,
		Secrets: repository.AppSecrets{
			AppSecret:      req.AppSecret,
			Token:          token,
			EncodingAESKey: aesKey,
		},
		ReplaceSecrets: true,
		ActorID:        req.ActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.create", err)
		return nil, err
	}
	return &PushSecretReveal{App: present(created), Token: token, EncodingAESKey: aesKey}, nil
}

func (s *appImpl) Get(ctx context.Context, id int64) (*entity.App, error) {
	app, err := s.apps.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.get", err)
		return nil, err
	}
	return present(app), nil
}

func (s *appImpl) List(ctx context.Context, q listq.Query) ([]*entity.App, error) {
	apps, err := s.apps.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.list", err)
		return nil, err
	}
	for i := range apps {
		apps[i] = present(apps[i])
	}
	return apps, nil
}

func (s *appImpl) Update(ctx context.Context, req UpdateDirectHoldRequest) (*entity.App, error) {
	if err := validateAppName(req.Name); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
		return nil, err
	}
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
		return nil, err
	}
	if req.AppID != "" && req.AppID != current.AppID {
		err = wechat.ErrAppIDImmutable
		logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
		return nil, err
	}
	if current.Connection != entity.ConnectionDirectHold {
		if req.AppSecret != "" {
			err = wechat.ErrAppInvalid
			logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
			return nil, err
		}
		current.Name = req.Name
		current.UpdateBy = req.ActorID
		updated, err := s.apps.Update(ctx, repository.SaveAppRequest{App: current, ActorID: req.ActorID})
		if err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
			return nil, err
		}
		return present(updated), nil
	}
	save := repository.SaveAppRequest{App: current, ActorID: req.ActorID}
	if req.AppSecret != "" {
		if err := validateAppSecret(req.AppSecret, true); err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
			return nil, err
		}
		stored, err := s.apps.LoadSecrets(ctx, req.ID)
		if err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
			return nil, err
		}
		stored.AppSecret = req.AppSecret
		current.AppSecretMask = maskSecret(stored.AppSecret)
		current.TokenMask = maskSecret(stored.Token)
		current.EncodingAESKeyMask = maskSecret(stored.EncodingAESKey)
		current.KeyVersion = entity.KeyVersion
		current.Lapsed = false
		save.Secrets = stored
		save.ReplaceSecrets = true
	}
	current.Name = req.Name
	current.UpdateBy = req.ActorID
	updated, err := s.apps.Update(ctx, save)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.update", err)
		return nil, err
	}
	return present(updated), nil
}

func (s *appImpl) RegeneratePushSecret(ctx context.Context, req RegeneratePushSecretRequest) (*PushSecretReveal, error) {
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.regenerate_push_secret", err)
		return nil, err
	}
	if current.Connection != entity.ConnectionDirectHold {
		err = wechat.ErrAppInvalid
		logx.Propagate(ctx, s.logger, "wechat.svc.app.regenerate_push_secret", err)
		return nil, err
	}
	stored, err := s.apps.LoadSecrets(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.regenerate_push_secret", err)
		return nil, err
	}
	token, aesKey, err := generatePushSecret()
	if err != nil {
		err = wechat.ErrSecretCrypto
		logx.Propagate(ctx, s.logger, "wechat.svc.app.regenerate_push_secret", err)
		return nil, err
	}
	stored.Token = token
	stored.EncodingAESKey = aesKey
	current.AppSecretMask = maskSecret(stored.AppSecret)
	current.TokenMask = maskSecret(token)
	current.EncodingAESKeyMask = maskSecret(aesKey)
	current.KeyVersion = entity.KeyVersion
	current.UpdateBy = req.ActorID
	updated, err := s.apps.Update(ctx, repository.SaveAppRequest{
		App:            current,
		Secrets:        stored,
		ReplaceSecrets: true,
		ActorID:        req.ActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.regenerate_push_secret", err)
		return nil, err
	}
	return &PushSecretReveal{App: present(updated), Token: token, EncodingAESKey: aesKey}, nil
}

func (s *appImpl) SetSuspended(ctx context.Context, req SetSuspendedRequest) (*entity.App, error) {
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.set_suspended", err)
		return nil, err
	}
	current.Suspended = req.Suspended
	current.UpdateBy = req.ActorID
	updated, err := s.apps.Update(ctx, repository.SaveAppRequest{
		App:     current,
		ActorID: req.ActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.set_suspended", err)
		return nil, err
	}
	return present(updated), nil
}

func (s *appImpl) SaveMenu(ctx context.Context, req SaveMenuRequest) (*entity.App, error) {
	const op = "wechat.svc.app.save_menu"
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := menuAllowed(current); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := validateMenu(req.Menu); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	menu := cloneMenu(req.Menu)
	current.SavedMenu = &menu
	current.UpdateBy = req.ActorID
	updated, err := s.apps.Update(ctx, repository.SaveAppRequest{
		App:     current,
		ActorID: req.ActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return present(updated), nil
}

func (s *appImpl) Withdraw(ctx context.Context, req WithdrawAppRequest) error {
	if err := s.apps.Delete(ctx, repository.DeleteAppRequest{
		ID:      req.ID,
		ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.withdraw", err)
		return err
	}
	return nil
}

func (s *appImpl) ensureAppIDAvailable(ctx context.Context, appID string) error {
	existing, err := s.apps.FindLiveByAppID(ctx, appID)
	if err != nil {
		if errors.Is(err, wechat.ErrAppNotFound) {
			return nil
		}
		return err
	}
	if existing != nil {
		return wechat.ErrAppDuplicate
	}
	return nil
}

func validateAppName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return wechat.ErrAppInvalid
	}
	return nil
}

func validateAppID(appID string) error {
	if appID == "" || utf8.RuneCountInString(appID) > maxAppIDLen {
		return wechat.ErrAppInvalid
	}
	return nil
}

func validateAppSecret(v string, required bool) error {
	if v == "" {
		if required {
			return wechat.ErrAppInvalid
		}
		return nil
	}
	if utf8.RuneCountInString(v) > maxSecretLen {
		return wechat.ErrAppInvalid
	}
	return nil
}

func validateDirectHoldShape(appType, kind string) error {
	switch appType {
	case entity.AppTypeMiniProgram:
		if kind != "" {
			return wechat.ErrAppInvalid
		}
		return nil
	case entity.AppTypeOfficialAccount:
		if kind != entity.AccountKindService && kind != entity.AccountKindSubscription {
			return wechat.ErrAppInvalid
		}
		return nil
	default:
		return wechat.ErrAppInvalid
	}
}

// callbackAddress is the operator-visible callback of one Direct Hold. It
// is derived from the row id, so a new row that reuses an AppID after
// withdraw shows a different address. The deployment's public origin is
// not stored on the row.
func callbackAddress(id int64) string {
	return fmt.Sprintf("/wechat/apps/%d/callback", id)
}

func present(app *entity.App) *entity.App {
	if app == nil {
		return nil
	}
	app.CallbackAddress = ""
	if app.Connection == entity.ConnectionDirectHold {
		app.CallbackAddress = callbackAddress(app.ID)
	}
	return app
}

// generatePushSecret mints the Token and EncodingAESKey an operator pastes
// into WeChat. The Token is 32 hexadecimal characters (inside WeChat's
// 3–32 alphanumeric limit). The EncodingAESKey is the unpadded base64 of
// 32 random bytes, retried until it stays inside A–Z, a–z, 0–9, which is
// the alphabet WeChat documents and still decodes to a 32-byte AES key.
func generatePushSecret() (string, string, error) {
	tokenRaw := make([]byte, 16)
	if _, err := rand.Read(tokenRaw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(tokenRaw)
	for range 128 {
		keyRaw := make([]byte, 32)
		if _, err := rand.Read(keyRaw); err != nil {
			return "", "", err
		}
		aesKey := trimBase64Padding(base64.StdEncoding.EncodeToString(keyRaw))
		if len(aesKey) == 43 && asciiAlnum(aesKey) {
			return token, aesKey, nil
		}
	}
	return "", "", errors.New("wechat: encoding aes key was not alphanumeric")
}

func trimBase64Padding(v string) string {
	for len(v) > 0 && v[len(v)-1] == '=' {
		v = v[:len(v)-1]
	}
	return v
}

func asciiAlnum(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
