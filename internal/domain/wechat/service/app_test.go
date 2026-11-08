package service

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/internal/infra/wechat/crypt"
	"glex/pkg/listq"
)

// memAppRepo is the in-memory repository the Direct Hold tests stand up.
// Writes seal secrets with the real cipher and never retain plaintext on
// the row the service reads back.
type memAppRepo struct {
	cipher *crypt.Cipher
	rows   map[int64]*memApp
	nextID int64
}

type memApp struct {
	app       *entity.App
	token     string
	withdrawn bool
}

func newMemAppRepo(cipher *crypt.Cipher) *memAppRepo {
	return &memAppRepo{
		cipher: cipher,
		rows:   map[int64]*memApp{},
		nextID: 200,
	}
}

func (r *memAppRepo) NextID() int64 { r.nextID++; return r.nextID }

func (r *memAppRepo) FindByID(_ context.Context, id int64) (*entity.App, error) {
	row, ok := r.rows[id]
	if !ok || row.app == nil || row.withdrawn {
		return nil, wechat.ErrAppNotFound
	}
	return cloneStoredApp(row.app), nil
}

func (r *memAppRepo) List(context.Context, listq.Query) ([]*entity.App, error) {
	out := make([]*entity.App, 0, len(r.rows))
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		out = append(out, cloneStoredApp(row.app))
	}
	return out, nil
}

func (r *memAppRepo) FindLiveByAppID(_ context.Context, appID string) (*entity.App, error) {
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		if row.app.AppID == appID {
			return cloneStoredApp(row.app), nil
		}
	}
	return nil, wechat.ErrAppNotFound
}

func (r *memAppRepo) CountLiveByPlatform(_ context.Context, platformID int64) (int, error) {
	n := 0
	for _, row := range r.rows {
		if row.withdrawn || row.app == nil {
			continue
		}
		if row.app.Connection == entity.ConnectionPlatformAuthorization && row.app.PlatformID == platformID {
			n++
		}
	}
	return n, nil
}

func (r *memAppRepo) LoadSecrets(_ context.Context, id int64) (repository.AppSecrets, error) {
	row, ok := r.rows[id]
	if !ok || row.withdrawn {
		return repository.AppSecrets{}, wechat.ErrAppNotFound
	}
	values, err := r.cipher.Decrypt(row.token)
	if err != nil {
		if errors.Is(err, crypt.ErrMasterKeyMissing) {
			return repository.AppSecrets{}, wechat.ErrMasterKeyMissing
		}
		return repository.AppSecrets{}, wechat.ErrSecretCrypto
	}
	return repository.AppSecrets{
		AppSecret:              values["app_secret"],
		Token:                  values["token"],
		EncodingAESKey:         values["encoding_aes_key"],
		AuthorizerRefreshToken: values["authorizer_refresh_token"],
	}, nil
}

func (r *memAppRepo) seal(secrets repository.AppSecrets) (string, error) {
	token, err := r.cipher.Encrypt(map[string]string{
		"app_secret":               secrets.AppSecret,
		"token":                    secrets.Token,
		"encoding_aes_key":         secrets.EncodingAESKey,
		"authorizer_refresh_token": secrets.AuthorizerRefreshToken,
	})
	if errors.Is(err, crypt.ErrMasterKeyMissing) {
		return "", wechat.ErrMasterKeyMissing
	}
	if err != nil {
		return "", wechat.ErrSecretCrypto
	}
	return token, nil
}

func (r *memAppRepo) Create(_ context.Context, req repository.SaveAppRequest) (*entity.App, error) {
	token, err := r.seal(req.Secrets)
	if err != nil {
		return nil, err
	}
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		if row.app.AppID == req.App.AppID {
			return nil, wechat.ErrAppDuplicate
		}
	}
	stored := cloneStoredApp(req.App)
	r.rows[stored.ID] = &memApp{app: stored, token: token}
	return cloneStoredApp(stored), nil
}

func (r *memAppRepo) Update(_ context.Context, req repository.SaveAppRequest) (*entity.App, error) {
	row, ok := r.rows[req.App.ID]
	if !ok || row.withdrawn {
		return nil, wechat.ErrAppNotFound
	}
	cp := cloneStoredApp(req.App)
	if req.ReplaceSecrets {
		token, err := r.seal(req.Secrets)
		if err != nil {
			return nil, err
		}
		row.token = token
	}
	cp.UpdateBy = req.ActorID
	row.app = cp
	return cloneStoredApp(cp), nil
}

func (r *memAppRepo) Delete(_ context.Context, req repository.DeleteAppRequest) error {
	row, ok := r.rows[req.ID]
	if !ok || row.withdrawn {
		return wechat.ErrAppNotFound
	}
	row.withdrawn = true
	row.app.UpdateBy = req.ActorID
	return nil
}

const testAppSecret = "app-secret-value-DDDD"

func TestDirectHoldMiniProgramRevealsPushSecretOnce(t *testing.T) {
	svc := newTestAppService(t)
	reveal, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	app := reveal.App
	if app == nil {
		t.Fatal("create returned no app")
	}
	if app.Name != "演示小程序" || app.AppID != "wxminiappid000001" {
		t.Fatalf("created app = %+v", app)
	}
	if app.AppType != entity.AppTypeMiniProgram || app.Kind != "" {
		t.Fatalf("mini program type/kind = %q/%q", app.AppType, app.Kind)
	}
	if app.Connection != entity.ConnectionDirectHold {
		t.Fatalf("connection = %q", app.Connection)
	}
	if app.Suspended {
		t.Fatal("a new direct hold is suspended")
	}
	if app.CreateBy != 7 || app.UpdateBy != 7 {
		t.Fatalf("audit = create_by %d update_by %d", app.CreateBy, app.UpdateBy)
	}
	assertGeneratedPushSecret(t, reveal.Token, reveal.EncodingAESKey)
	if reveal.Token == testAppSecret || reveal.EncodingAESKey == testAppSecret {
		t.Fatal("push secret echoed the app secret")
	}
	assertAppMasks(t, app, testAppSecret, reveal.Token, reveal.EncodingAESKey)

	got, err := svc.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertAppMasks(t, got, testAppSecret, reveal.Token, reveal.EncodingAESKey)

	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := findApp(listed, app.ID)
	if found == nil {
		t.Fatalf("live list = %+v, want the new mini program", listed)
	}
	assertAppMasks(t, found, testAppSecret, reveal.Token, reveal.EncodingAESKey)
}

func TestDirectHoldRejectsIncomplete(t *testing.T) {
	svc := newTestAppService(t)
	base := miniHold("wxminiappid000001", 7)
	cases := []CreateDirectHoldRequest{
		without(base, func(r *CreateDirectHoldRequest) { r.Name = "" }),
		without(base, func(r *CreateDirectHoldRequest) { r.AppID = "" }),
		without(base, func(r *CreateDirectHoldRequest) { r.AppSecret = "" }),
		without(base, func(r *CreateDirectHoldRequest) { r.AppType = "" }),
		without(base, func(r *CreateDirectHoldRequest) { r.Name = strings.Repeat("名", 65) }),
		without(base, func(r *CreateDirectHoldRequest) { r.AppID = strings.Repeat("a", 65) }),
		without(base, func(r *CreateDirectHoldRequest) { r.AppSecret = strings.Repeat("s", 257) }),
	}
	for _, in := range cases {
		if _, err := svc.Create(context.Background(), in); !errors.Is(err, wechat.ErrAppInvalid) {
			t.Fatalf("create %+v err = %v, want ErrAppInvalid", in, err)
		}
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("incomplete direct hold stored %d apps", len(listed))
	}
}

func TestOfficialAccountRequiresKind(t *testing.T) {
	svc := newTestAppService(t)
	official := miniHold("wxofficial000001", 7)
	official.Name = "未标明公众号"
	official.AppType = entity.AppTypeOfficialAccount
	if _, err := svc.Create(context.Background(), official); !errors.Is(err, wechat.ErrAppInvalid) {
		t.Fatalf("official without kind err = %v, want ErrAppInvalid", err)
	}
	bogus := official
	bogus.Kind = "enterprise"
	if _, err := svc.Create(context.Background(), bogus); !errors.Is(err, wechat.ErrAppInvalid) {
		t.Fatalf("official with unknown kind err = %v, want ErrAppInvalid", err)
	}
	miniWithKind := miniHold("wxminiappid000001", 7)
	miniWithKind.Kind = entity.AccountKindService
	if _, err := svc.Create(context.Background(), miniWithKind); !errors.Is(err, wechat.ErrAppInvalid) {
		t.Fatalf("mini program with a kind err = %v, want ErrAppInvalid", err)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("rejected holds stored %+v", listed)
	}

	service := official
	service.Name = "服务号甲"
	service.Kind = entity.AccountKindService
	serviceReveal, err := svc.Create(context.Background(), service)
	if err != nil {
		t.Fatalf("service account: %v", err)
	}
	if serviceReveal.App.AppType != entity.AppTypeOfficialAccount || serviceReveal.App.Kind != entity.AccountKindService {
		t.Fatalf("service account = %+v", serviceReveal.App)
	}
	assertGeneratedPushSecret(t, serviceReveal.Token, serviceReveal.EncodingAESKey)
	assertAppMasks(t, serviceReveal.App, testAppSecret, serviceReveal.Token, serviceReveal.EncodingAESKey)

	subscription := official
	subscription.Name = "订阅号乙"
	subscription.AppID = "wxofficial000002"
	subscription.Kind = entity.AccountKindSubscription
	subscriptionReveal, err := svc.Create(context.Background(), subscription)
	if err != nil {
		t.Fatalf("subscription account: %v", err)
	}
	if subscriptionReveal.App.Kind != entity.AccountKindSubscription || subscriptionReveal.App.AppType != entity.AppTypeOfficialAccount {
		t.Fatalf("subscription account = %+v", subscriptionReveal.App)
	}
	if subscriptionReveal.App.ID == serviceReveal.App.ID {
		t.Fatal("subscription reused the service account id")
	}
}

func TestEachDirectHoldHasItsOwnCallback(t *testing.T) {
	svc := newTestAppService(t)
	first, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := svc.Create(context.Background(), miniHold("wxminiappid000002", 7))
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.App.CallbackAddress == "" || second.App.CallbackAddress == "" {
		t.Fatalf("callbacks = %q, %q", first.App.CallbackAddress, second.App.CallbackAddress)
	}
	if first.App.CallbackAddress == second.App.CallbackAddress {
		t.Fatalf("two direct holds share callback %q", first.App.CallbackAddress)
	}
	original := first.App.CallbackAddress

	renamed, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      first.App.ID,
		Name:    "改了名称",
		ActorID: 8,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.CallbackAddress != original {
		t.Fatalf("rename changed callback to %q", renamed.CallbackAddress)
	}

	suspended, err := svc.SetSuspended(context.Background(), SetSuspendedRequest{
		ID:        first.App.ID,
		Suspended: true,
		ActorID:   8,
	})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspended.CallbackAddress != original {
		t.Fatalf("suspend changed callback to %q", suspended.CallbackAddress)
	}

	rotated, err := svc.RegeneratePushSecret(context.Background(), RegeneratePushSecretRequest{
		ID:      first.App.ID,
		ActorID: 8,
	})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if rotated.App.CallbackAddress != original {
		t.Fatalf("regenerate changed callback to %q", rotated.App.CallbackAddress)
	}

	replaced, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:        first.App.ID,
		Name:      "改了名称",
		AppSecret: "app-secret-value-EEEE",
		ActorID:   8,
	})
	if err != nil {
		t.Fatalf("replace secret: %v", err)
	}
	if replaced.CallbackAddress != original {
		t.Fatalf("secret replacement changed callback to %q", replaced.CallbackAddress)
	}

	got, err := svc.Get(context.Background(), first.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CallbackAddress != original {
		t.Fatalf("stored callback = %q, want %q", got.CallbackAddress, original)
	}
	again, err := svc.Get(context.Background(), second.App.ID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if again.CallbackAddress != second.App.CallbackAddress {
		t.Fatalf("second callback changed from %q to %q", second.App.CallbackAddress, again.CallbackAddress)
	}
}

func TestRegeneratePushSecretRevealsOnceAndKeepsTheApp(t *testing.T) {
	svc := newTestAppService(t)
	created, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	suspended, err := svc.SetSuspended(context.Background(), SetSuspendedRequest{
		ID:        created.App.ID,
		Suspended: true,
		ActorID:   8,
	})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if !suspended.Suspended {
		t.Fatal("suspend did not stick")
	}

	rotated, err := svc.RegeneratePushSecret(context.Background(), RegeneratePushSecretRequest{
		ID:      created.App.ID,
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	assertGeneratedPushSecret(t, rotated.Token, rotated.EncodingAESKey)
	if rotated.Token == created.Token || rotated.EncodingAESKey == created.EncodingAESKey {
		t.Fatalf("regenerate returned the previous pair")
	}
	app := rotated.App
	if app.ID != created.App.ID || app.AppID != created.App.AppID || app.Name != created.App.Name {
		t.Fatalf("regenerate changed the app: %+v", app)
	}
	if app.Connection != entity.ConnectionDirectHold || !app.Suspended {
		t.Fatalf("regenerate changed connection or suspension: %+v", app)
	}
	if app.UpdateBy != 9 {
		t.Fatalf("update_by = %d, want 9", app.UpdateBy)
	}
	assertAppMasks(t, app, testAppSecret, rotated.Token, rotated.EncodingAESKey)
	assertNoPlaintext(t, app, created.Token, created.EncodingAESKey)

	got, err := svc.Get(context.Background(), created.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.App.ID || !got.Suspended || got.Connection != entity.ConnectionDirectHold {
		t.Fatalf("app after regenerate = %+v", got)
	}
	assertAppMasks(t, got, testAppSecret, rotated.Token, rotated.EncodingAESKey)
	assertNoPlaintext(t, got, created.Token, created.EncodingAESKey, rotated.Token, rotated.EncodingAESKey)

	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := findApp(listed, created.App.ID)
	if found == nil || !found.Suspended {
		t.Fatalf("live list after regenerate = %+v", listed)
	}
	assertAppMasks(t, found, testAppSecret, rotated.Token, rotated.EncodingAESKey)
	assertNoPlaintext(t, found, created.Token, created.EncodingAESKey, rotated.Token, rotated.EncodingAESKey)
}

func TestReplaceAppSecretDoesNotReturnPlaintext(t *testing.T) {
	svc := newTestAppService(t)
	created, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const fresh = "app-secret-value-ZZZZ"
	updated, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:        created.App.ID,
		Name:      "演示小程序",
		AppSecret: fresh,
		ActorID:   8,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != created.App.ID || updated.AppID != created.App.AppID || updated.Suspended {
		t.Fatalf("replacement changed the app: %+v", updated)
	}
	if updated.UpdateBy != 8 {
		t.Fatalf("update_by = %d", updated.UpdateBy)
	}
	assertAppMasks(t, updated, fresh, created.Token, created.EncodingAESKey)
	assertNoPlaintext(t, updated, fresh, testAppSecret, created.Token, created.EncodingAESKey)

	got, err := svc.Get(context.Background(), created.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertAppMasks(t, got, fresh, created.Token, created.EncodingAESKey)
	assertNoPlaintext(t, got, fresh, testAppSecret)

	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := findApp(listed, created.App.ID)
	if found == nil {
		t.Fatal("replaced app left the live list")
	}
	assertAppMasks(t, found, fresh, created.Token, created.EncodingAESKey)
	assertNoPlaintext(t, found, fresh)
}

func TestNameCanChangeAndAppIDCannot(t *testing.T) {
	svc := newTestAppService(t)
	created, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	renamed, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      created.App.ID,
		Name:    "新的名称",
		AppID:   created.App.AppID,
		ActorID: 8,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "新的名称" || renamed.AppID != created.App.AppID || renamed.UpdateBy != 8 {
		t.Fatalf("renamed = %+v", renamed)
	}
	assertAppMasks(t, renamed, testAppSecret, created.Token, created.EncodingAESKey)

	if _, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      created.App.ID,
		Name:    "不该改成这个",
		AppID:   "wxminiappid999999",
		ActorID: 8,
	}); !errors.Is(err, wechat.ErrAppIDImmutable) {
		t.Fatalf("err = %v, want ErrAppIDImmutable", err)
	}
	got, err := svc.Get(context.Background(), created.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "新的名称" || got.AppID != "wxminiappid000001" {
		t.Fatalf("immutable update changed the row: %+v", got)
	}

	if _, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      created.App.ID,
		ActorID: 8,
	}); !errors.Is(err, wechat.ErrAppInvalid) {
		t.Fatalf("blank name err = %v, want ErrAppInvalid", err)
	}
	got, err = svc.Get(context.Background(), created.App.ID)
	if err != nil {
		t.Fatalf("get after blank name: %v", err)
	}
	if got.Name != "新的名称" {
		t.Fatalf("blank name changed the stored name to %q", got.Name)
	}
}

func TestSuspendAndResumeIsNotWithdrawal(t *testing.T) {
	svc := newTestAppService(t)
	created, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	suspended, err := svc.SetSuspended(context.Background(), SetSuspendedRequest{
		ID:        created.App.ID,
		Suspended: true,
		ActorID:   8,
	})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if !suspended.Suspended || suspended.ID != created.App.ID || suspended.Connection != entity.ConnectionDirectHold {
		t.Fatalf("suspended = %+v", suspended)
	}
	if suspended.UpdateBy != 8 {
		t.Fatalf("update_by = %d", suspended.UpdateBy)
	}
	assertAppMasks(t, suspended, testAppSecret, created.Token, created.EncodingAESKey)
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if findApp(listed, created.App.ID) == nil {
		t.Fatal("suspension removed the app from the live list")
	}
	got, err := svc.Get(context.Background(), created.App.ID)
	if err != nil {
		t.Fatalf("get suspended: %v", err)
	}
	if !got.Suspended {
		t.Fatal("get lost the suspension")
	}

	resumed, err := svc.SetSuspended(context.Background(), SetSuspendedRequest{
		ID:        created.App.ID,
		Suspended: false,
		ActorID:   9,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Suspended || resumed.ID != created.App.ID || resumed.UpdateBy != 9 {
		t.Fatalf("resumed = %+v", resumed)
	}
	assertAppMasks(t, resumed, testAppSecret, created.Token, created.EncodingAESKey)
	listed, err = svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list after resume: %v", err)
	}
	found := findApp(listed, created.App.ID)
	if found == nil || found.Suspended {
		t.Fatalf("live list after resume = %+v", listed)
	}
}

func TestWithdrawOmitsAppFromLiveList(t *testing.T) {
	svc := newTestAppService(t)
	created, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Withdraw(context.Background(), WithdrawAppRequest{ID: created.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if findApp(listed, created.App.ID) != nil {
		t.Fatalf("withdrawn app still live: %+v", listed)
	}
	if _, err := svc.Get(context.Background(), created.App.ID); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("get withdrawn err = %v, want ErrAppNotFound", err)
	}
	if err := svc.Withdraw(context.Background(), WithdrawAppRequest{ID: created.App.ID, ActorID: 9}); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("second withdraw err = %v, want ErrAppNotFound", err)
	}
	if _, err := svc.RegeneratePushSecret(context.Background(), RegeneratePushSecretRequest{ID: created.App.ID, ActorID: 9}); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("regenerate withdrawn err = %v, want ErrAppNotFound", err)
	}
	if _, err := svc.SetSuspended(context.Background(), SetSuspendedRequest{ID: created.App.ID, Suspended: true, ActorID: 9}); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("suspend withdrawn err = %v, want ErrAppNotFound", err)
	}
}

func TestDuplicateLiveAppIDIsRejected(t *testing.T) {
	svc := newTestAppService(t)
	first, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	again := miniHold("wxminiappid000001", 8)
	again.Name = "另一条"
	if _, err := svc.Create(context.Background(), again); !errors.Is(err, wechat.ErrAppDuplicate) {
		t.Fatalf("err = %v, want ErrAppDuplicate", err)
	}
	second, err := svc.Create(context.Background(), miniHold("wxminiappid000002", 8))
	if err != nil {
		t.Fatalf("different app id: %v", err)
	}
	if second.App.ID == first.App.ID {
		t.Fatalf("second app reused id %d", second.App.ID)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live apps = %d, want 2", len(listed))
	}
}

func TestReconnectAfterWithdrawIsANewRow(t *testing.T) {
	svc := newTestAppService(t)
	first, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Withdraw(context.Background(), WithdrawAppRequest{ID: first.App.ID, ActorID: 7}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	again := miniHold("wxminiappid000001", 8)
	again.Name = "重新接入"
	again.AppSecret = "app-secret-value-NNNN"
	second, err := svc.Create(context.Background(), again)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if second.App.ID == first.App.ID || second.App.AppID != first.App.AppID || second.App.Name != "重新接入" {
		t.Fatalf("reconnected = %+v, first id %d", second.App, first.App.ID)
	}
	if second.App.CallbackAddress == "" || second.App.CallbackAddress == first.App.CallbackAddress {
		t.Fatalf("reconnected callback = %q, first = %q", second.App.CallbackAddress, first.App.CallbackAddress)
	}
	assertGeneratedPushSecret(t, second.Token, second.EncodingAESKey)
	if second.Token == first.Token && second.EncodingAESKey == first.EncodingAESKey {
		t.Fatal("reconnected app reused the withdrawn push secret")
	}
	assertAppMasks(t, second.App, again.AppSecret, second.Token, second.EncodingAESKey)
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != second.App.ID {
		t.Fatalf("live list = %+v, want only the new row", listed)
	}
	if _, err := svc.Get(context.Background(), first.App.ID); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("withdrawn row still readable: %v", err)
	}
}

func TestAppIDUniquenessIsIndependentOfPlatform(t *testing.T) {
	const shared = "wxsharedappid0001"
	platforms := newTestPlatformService(t)
	apps := newTestAppService(t)
	if _, err := platforms.Create(context.Background(), CreateRequest{
		Name:            "华东平台",
		ComponentAppID:  shared,
		ComponentSecret: testComponentSecret,
		Token:           testToken,
		EncodingAESKey:  testEncodingAESKey,
		ActorID:         7,
	}); err != nil {
		t.Fatalf("platform: %v", err)
	}
	created, err := apps.Create(context.Background(), miniHold(shared, 7))
	if err != nil {
		t.Fatalf("app with the platform component AppID: %v", err)
	}
	if created.App.AppID != shared || created.App.Connection != entity.ConnectionDirectHold {
		t.Fatalf("created = %+v", created.App)
	}
	if _, err := apps.Create(context.Background(), miniHold(shared, 8)); !errors.Is(err, wechat.ErrAppDuplicate) {
		t.Fatalf("second app err = %v, want ErrAppDuplicate", err)
	}
}

func TestMissingMasterKeyRefusesAppSecretWrites(t *testing.T) {
	t.Setenv(crypt.EnvMasterKey, "")
	svc := NewAppService(newMemAppRepo(crypt.New()), nil, nil, nil, newMemHandoffRepo(), nil, "", slog.New(slog.DiscardHandler))
	created, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if !errors.Is(err, wechat.ErrMasterKeyMissing) || created != nil {
		t.Fatalf("create = (%v, %v), want master-key refusal and no row", created, err)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("refused create stored %+v", listed)
	}

	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	repo := newMemAppRepo(crypt.New())
	svc = NewAppService(repo, nil, nil, nil, newMemHandoffRepo(), nil, "", slog.New(slog.DiscardHandler))
	stored, err := svc.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("create with key: %v", err)
	}
	t.Setenv(crypt.EnvMasterKey, "")
	repo.cipher = crypt.New()

	const fresh = "app-secret-value-NNNN"
	replaced, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:        stored.App.ID,
		Name:      "演示小程序",
		AppSecret: fresh,
		ActorID:   9,
	})
	if !errors.Is(err, wechat.ErrMasterKeyMissing) || replaced != nil {
		t.Fatalf("replace = (%v, %v), want master-key refusal", replaced, err)
	}
	rotated, err := svc.RegeneratePushSecret(context.Background(), RegeneratePushSecretRequest{
		ID:      stored.App.ID,
		ActorID: 9,
	})
	if !errors.Is(err, wechat.ErrMasterKeyMissing) || rotated != nil {
		t.Fatalf("regenerate = (%v, %v), want master-key refusal", rotated, err)
	}
	got, err := svc.Get(context.Background(), stored.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertAppMasks(t, got, testAppSecret, stored.Token, stored.EncodingAESKey)
	assertNoPlaintext(t, got, fresh)
	if got.Suspended || got.ID != stored.App.ID {
		t.Fatalf("refused rotation changed the app: %+v", got)
	}

	renamed, err := svc.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      stored.App.ID,
		Name:    "只改名称",
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("rename without master key: %v", err)
	}
	if renamed.Name != "只改名称" {
		t.Fatalf("renamed = %+v", renamed)
	}
	assertAppMasks(t, renamed, testAppSecret, stored.Token, stored.EncodingAESKey)

	suspended, err := svc.SetSuspended(context.Background(), SetSuspendedRequest{
		ID:        stored.App.ID,
		Suspended: true,
		ActorID:   9,
	})
	if err != nil {
		t.Fatalf("suspend without master key: %v", err)
	}
	if !suspended.Suspended || suspended.Name != "只改名称" {
		t.Fatalf("suspended = %+v", suspended)
	}
	assertAppMasks(t, suspended, testAppSecret, stored.Token, stored.EncodingAESKey)
}

func miniHold(appID string, actorID int64) CreateDirectHoldRequest {
	return CreateDirectHoldRequest{
		Name:      "演示小程序",
		AppID:     appID,
		AppType:   entity.AppTypeMiniProgram,
		AppSecret: testAppSecret,
		ActorID:   actorID,
	}
}

func without(in CreateDirectHoldRequest, edit func(*CreateDirectHoldRequest)) CreateDirectHoldRequest {
	edit(&in)
	return in
}

func newTestAppService(t *testing.T) App {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	repo := newMemAppRepo(crypt.New())
	return NewAppService(repo, nil, nil, nil, newMemHandoffRepo(), nil, "", slog.New(slog.DiscardHandler))
}

func cloneStoredApp(app *entity.App) *entity.App {
	if app == nil {
		return nil
	}
	cp := *app
	cp.CallbackAddress = ""
	if app.Capabilities != nil {
		cp.Capabilities = append([]string(nil), app.Capabilities...)
	}
	if app.SavedMenu != nil {
		menu := cloneMenu(*app.SavedMenu)
		cp.SavedMenu = &menu
	}
	if app.AcceptedMenu != nil {
		menu := cloneMenu(*app.AcceptedMenu)
		cp.AcceptedMenu = &menu
	}
	return &cp
}

func findApp(apps []*entity.App, id int64) *entity.App {
	for _, app := range apps {
		if app != nil && app.ID == id {
			return app
		}
	}
	return nil
}

func assertGeneratedPushSecret(t *testing.T, token, aesKey string) {
	t.Helper()
	if len(token) < 3 || len(token) > 32 || !isASCIIAlnum(token) {
		t.Fatalf("token %q is not a 3-32 character alphanumeric WeChat token", token)
	}
	if len(aesKey) != 43 || !isASCIIAlnum(aesKey) {
		t.Fatalf("encoding aes key %q is not a 43-character alphanumeric WeChat key", aesKey)
	}
	raw, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil || len(raw) != 32 {
		t.Fatalf("encoding aes key does not decode to a 32-byte AES key: %v len %d", err, len(raw))
	}
}

func isASCIIAlnum(v string) bool {
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

func assertAppMasks(t *testing.T, app *entity.App, secret, token, aesKey string) {
	t.Helper()
	if app == nil {
		t.Fatal("app is nil")
	}
	if app.AppSecretMask != maskOf(secret) || app.TokenMask != maskOf(token) || app.EncodingAESKeyMask != maskOf(aesKey) {
		t.Fatalf("masks = secret %q token %q aes %q, want %q %q %q",
			app.AppSecretMask, app.TokenMask, app.EncodingAESKeyMask,
			maskOf(secret), maskOf(token), maskOf(aesKey))
	}
	assertNoPlaintext(t, app, secret, token, aesKey)
}

func assertNoPlaintext(t *testing.T, app *entity.App, plains ...string) {
	t.Helper()
	if app == nil {
		t.Fatal("app is nil")
	}
	rendered := strings.Join([]string{
		app.Name,
		app.AppID,
		app.AppType,
		app.Kind,
		app.Connection,
		app.AppSecretMask,
		app.TokenMask,
		app.EncodingAESKeyMask,
		app.CallbackAddress,
	}, "\n")
	for _, plain := range plains {
		if plain != "" && strings.Contains(rendered, plain) {
			t.Fatalf("app exposes plaintext %q in %+v", plain, app)
		}
	}
}

type authFixture struct {
	apps      App
	platforms Platform
	api       *scriptedOpenAPI
	appRepo   *memAppRepo
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	api := &scriptedOpenAPI{}
	platformRepo := newMemPlatformRepo(crypt.New())
	appRepo := newMemAppRepo(crypt.New())
	logger := slog.New(slog.DiscardHandler)
	apps := NewAppService(appRepo, platformRepo, newMemTemplateRepo(), newMemDispatchRepo(), newMemHandoffRepo(), api, testCallbackBase, logger)
	platforms := NewPlatformService(platformRepo, api, NewPushAuthenticator(), testCallbackBase, apps, logger)
	return &authFixture{apps: apps, platforms: platforms, api: api, appRepo: appRepo}
}

func (f *authFixture) holdReady(t *testing.T, name, componentAppID string) *entity.Platform {
	t.Helper()
	created, err := f.platforms.Create(context.Background(), CreateRequest{
		Name:            name,
		ComponentAppID:  componentAppID,
		ComponentSecret: testComponentSecret,
		Token:           testPushToken,
		EncodingAESKey:  testPushAESKey,
		ActorID:         7,
	})
	if err != nil {
		t.Fatalf("register platform: %v", err)
	}
	_, err = f.platforms.Receive(context.Background(), encryptedTicketPush(t, created.ID, componentAppID, "ticket-"+componentAppID))
	if err != nil {
		t.Fatalf("ticket: %v", err)
	}
	got := mustGet(t, f.platforms, created.ID)
	if !got.Ready {
		t.Fatal("platform with a verify ticket is not ready")
	}
	return got
}

func (f *authFixture) script(grant AuthorizationGrant) {
	f.api.grant = &grant
	f.api.grantErr = nil
}

func TestStartAuthorizationRequiresReadyPlatform(t *testing.T) {
	f := newAuthFixture(t)
	unready, err := f.platforms.Create(context.Background(), CreateRequest{
		Name:            "未就绪平台",
		ComponentAppID:  "wxcompappid0000un",
		ComponentSecret: testComponentSecret,
		Token:           testToken,
		EncodingAESKey:  testEncodingAESKey,
		ActorID:         7,
	})
	if err != nil {
		t.Fatalf("register unready: %v", err)
	}
	if unready.Ready {
		t.Fatal("a platform without a verify ticket is ready")
	}
	link, err := f.apps.StartAuthorization(context.Background(), StartAuthorizationRequest{PlatformID: unready.ID})
	if !errors.Is(err, wechat.ErrPlatformNotReady) || link != nil {
		t.Fatalf("unready start = (%v, %v), want not ready", link, err)
	}

	ready := f.holdReady(t, "华东平台", "wxcompappid000001")
	if err := f.platforms.Withdraw(context.Background(), WithdrawRequest{ID: ready.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw platform: %v", err)
	}
	link, err = f.apps.StartAuthorization(context.Background(), StartAuthorizationRequest{PlatformID: ready.ID})
	if !errors.Is(err, wechat.ErrPlatformNotFound) || link != nil {
		t.Fatalf("withdrawn start = (%v, %v), want not found", link, err)
	}

	live := f.holdReady(t, "华南平台", "wxcompappid000002")
	f.api.authLink = "https://mp.weixin.qq.com/cgi-bin/componentloginpage?pre_auth_code=scripted-live"
	link, err = f.apps.StartAuthorization(context.Background(), StartAuthorizationRequest{PlatformID: live.ID})
	if err != nil {
		t.Fatalf("ready start: %v", err)
	}
	if link == nil || link.URL != f.api.authLink {
		t.Fatalf("link = %+v", link)
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("starting authorization created %+v", listed)
	}
}

func TestCompletedGrantCreatesAppFromWeChat(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxminiappid000099",
		Name:         "授权小程序",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilityMiniProgramLogin, entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-RRRR",
	})
	created, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-mini",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if created.ID == 0 || created.Name != "授权小程序" || created.AppID != "wxminiappid000099" {
		t.Fatalf("created = %+v", created)
	}
	if created.AppType != entity.AppTypeMiniProgram || created.Kind != "" {
		t.Fatalf("type/kind = %q/%q", created.AppType, created.Kind)
	}
	if created.Connection != entity.ConnectionPlatformAuthorization || created.PlatformID != platform.ID {
		t.Fatalf("binding = %q platform %d, want platform %d", created.Connection, created.PlatformID, platform.ID)
	}
	if !sameCaps(created.Capabilities, []string{entity.CapabilityMiniProgramLogin, entity.CapabilitySubscribeMessage}) {
		t.Fatalf("capabilities = %#v", created.Capabilities)
	}
	if created.CreateBy != 0 || created.UpdateBy != 0 {
		t.Fatalf("audit = create_by %d update_by %d, want the system", created.CreateBy, created.UpdateBy)
	}
	if created.AppSecretMask != "" || created.TokenMask != "" || created.EncodingAESKeyMask != "" || created.CallbackAddress != "" {
		t.Fatalf("platform authorization shows a direct-hold secret or callback: %+v", created)
	}
	if created.Suspended {
		t.Fatal("a new platform authorization is suspended")
	}
	secrets, err := f.appRepo.LoadSecrets(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("load secrets: %v", err)
	}
	if secrets.AppSecret != "" || secrets.Token != "" || secrets.EncodingAESKey != "" {
		t.Fatalf("stored an authorizer AppSecret or push secret: %+v", secrets)
	}
	if secrets.AuthorizerRefreshToken != "authorizer-refresh-RRRR" {
		t.Fatal("authorizer refresh material was not kept")
	}
	assertNoPlaintext(t, created, "authorizer-refresh-RRRR")

	got, err := f.apps.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PlatformID != platform.ID || got.Connection != entity.ConnectionPlatformAuthorization {
		t.Fatalf("reread = %+v", got)
	}

	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000099",
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityCustomMenu, entity.CapabilityTemplateMessage, entity.CapabilityWebAuthorization},
		RefreshToken: "authorizer-refresh-SSSS",
	})
	service, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-service",
	})
	if err != nil {
		t.Fatalf("service grant: %v", err)
	}
	if service.Kind != entity.AccountKindService || service.PlatformID != platform.ID || service.ID == created.ID {
		t.Fatalf("service = %+v", service)
	}

	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000088",
		Name:         "授权订阅号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindSubscription,
		Capabilities: []string{},
		RefreshToken: "authorizer-refresh-TTTT",
	})
	sub, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-sub",
	})
	if err != nil {
		t.Fatalf("subscription grant: %v", err)
	}
	if sub.Kind != entity.AccountKindSubscription || len(sub.Capabilities) != 0 {
		t.Fatalf("subscription = %+v", sub)
	}
}

func TestPlatformAuthorizationRefusesAppSecretAndPushSecret(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	app := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	before := describeApp(app)

	replaced, err := f.apps.Update(context.Background(), UpdateDirectHoldRequest{
		ID:        app.ID,
		Name:      "想改成直连",
		AppSecret: "app-secret-value-NNNN",
		ActorID:   9,
	})
	if !errors.Is(err, wechat.ErrAppInvalid) || replaced != nil {
		t.Fatalf("app secret = (%v, %v), want refusal", replaced, err)
	}
	rotated, err := f.apps.RegeneratePushSecret(context.Background(), RegeneratePushSecretRequest{ID: app.ID, ActorID: 9})
	if !errors.Is(err, wechat.ErrAppInvalid) || rotated != nil {
		t.Fatalf("regenerate = (%v, %v), want refusal", rotated, err)
	}
	got, err := f.apps.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if describeApp(got) != before {
		t.Fatalf("refused secret change altered the app\n got %s\nwant %s", describeApp(got), before)
	}

	renamed, err := f.apps.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      app.ID,
		Name:    "改个显示名",
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "改个显示名" || renamed.Connection != entity.ConnectionPlatformAuthorization || renamed.PlatformID != platform.ID {
		t.Fatalf("renamed = %+v", renamed)
	}
	if renamed.UpdateBy != 9 || renamed.AppSecretMask != "" {
		t.Fatalf("rename audit/mask = %+v", renamed)
	}
}

func TestOfficialAccountGrantWithoutKindCreatesNothing(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxofficial0000077",
		Name:         "种类不明",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         "",
		Capabilities: []string{entity.CapabilityCustomMenu},
		RefreshToken: "authorizer-refresh-UUUU",
	})
	created, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-nokind",
	})
	if !errors.Is(err, wechat.ErrAppInvalid) || created != nil {
		t.Fatalf("grant = (%v, %v), want no app", created, err)
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("kindless grant stored %+v", listed)
	}
}

func TestReauthorizationRestoresSameApp(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	first := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin, entity.CapabilitySubscribeMessage})
	renamed, err := f.apps.Update(context.Background(), UpdateDirectHoldRequest{
		ID:      first.ID,
		Name:    "运营改过的名字",
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	suspended, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: first.ID, Suspended: true, ActorID: 9})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	f.script(AuthorizationGrant{
		AppID:        first.AppID,
		Name:         "微信现在的名字",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-NEWW",
	})
	again, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-again",
	})
	if err != nil {
		t.Fatalf("reauthorize: %v", err)
	}
	if again.ID != first.ID || again.PlatformID != platform.ID || again.Connection != entity.ConnectionPlatformAuthorization {
		t.Fatalf("restored = %+v, first id %d platform %d", again, first.ID, platform.ID)
	}
	if again.Name != renamed.Name || !again.Suspended {
		t.Fatalf("reauthorization rewrote the operator's name or suspension: %+v", again)
	}
	if !sameCaps(again.Capabilities, []string{entity.CapabilitySubscribeMessage}) {
		t.Fatalf("capabilities = %#v", again.Capabilities)
	}
	if again.CreateBy != 0 || again.UpdateBy != 0 {
		t.Fatalf("audit = create_by %d update_by %d, want the system on the WeChat write", again.CreateBy, again.UpdateBy)
	}
	if suspended.ID != again.ID {
		t.Fatalf("suspension moved the row from %d to %d", suspended.ID, again.ID)
	}
	secrets, err := f.appRepo.LoadSecrets(context.Background(), again.ID)
	if err != nil {
		t.Fatalf("load secrets: %v", err)
	}
	if secrets.AppSecret != "" || secrets.AuthorizerRefreshToken != "authorizer-refresh-NEWW" {
		t.Fatalf("refresh material = app secret %q refresh %q", secrets.AppSecret, secrets.AuthorizerRefreshToken)
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != first.ID {
		t.Fatalf("live list = %+v, want the same row", listed)
	}
}

func TestGrantLeavesDirectHoldAndOtherPlatformUnchanged(t *testing.T) {
	f := newAuthFixture(t)
	east := f.holdReady(t, "华东平台", "wxcompappid000001")
	south := f.holdReady(t, "华南平台", "wxcompappid000002")
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	heldBefore := describeApp(held.App)
	f.script(AuthorizationGrant{
		AppID:        held.App.AppID,
		Name:         "想改成平台授权",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilityMiniProgramLogin},
		RefreshToken: "authorizer-refresh-HHHH",
	})
	created, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: east.ID,
		AuthCode:   "auth-code-conflict",
	})
	if !errors.Is(err, wechat.ErrAppDuplicate) || created != nil {
		t.Fatalf("direct-hold conflict = (%v, %v)", created, err)
	}
	got, err := f.apps.Get(context.Background(), held.App.ID)
	if err != nil {
		t.Fatalf("get direct hold: %v", err)
	}
	if describeApp(got) != heldBefore {
		t.Fatalf("direct hold changed\n got %s\nwant %s", describeApp(got), heldBefore)
	}

	bound := f.grantMini(t, east.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	boundBefore := describeApp(bound)
	f.script(AuthorizationGrant{
		AppID:        bound.AppID,
		Name:         "想换平台",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-MMMM",
	})
	moved, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: south.ID,
		AuthCode:   "auth-code-move",
	})
	if !errors.Is(err, wechat.ErrAppDuplicate) || moved != nil {
		t.Fatalf("move = (%v, %v)", moved, err)
	}
	got, err = f.apps.Get(context.Background(), bound.ID)
	if err != nil {
		t.Fatalf("get bound: %v", err)
	}
	if describeApp(got) != boundBefore {
		t.Fatalf("bound app changed\n got %s\nwant %s", describeApp(got), boundBefore)
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live apps = %d, want the direct hold and the one platform app", len(listed))
	}

	second, err := f.apps.Create(context.Background(), miniHold(bound.AppID, 8))
	if !errors.Is(err, wechat.ErrAppDuplicate) || second != nil {
		t.Fatalf("platform app becoming direct hold = (%v, %v)", second, err)
	}
	got, err = f.apps.Get(context.Background(), bound.ID)
	if err != nil {
		t.Fatalf("get after direct-hold attempt: %v", err)
	}
	if describeApp(got) != boundBefore {
		t.Fatalf("platform app changed into a direct hold\n got %s\nwant %s", describeApp(got), boundBefore)
	}
}

func TestWithdrawPlatformRefusedWhileAppAuthorized(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	older := f.grantMini(t, platform.ID, "wxminiappid000088", []string{entity.CapabilityMiniProgramLogin})
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: older.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw app: %v", err)
	}
	live := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilitySubscribeMessage})
	liveBefore := describeApp(live)
	err := f.platforms.Withdraw(context.Background(), WithdrawRequest{ID: platform.ID, ActorID: 9})
	if !errors.Is(err, wechat.ErrPlatformInUse) {
		t.Fatalf("withdraw platform err = %v, want in use", err)
	}
	if _, err := f.platforms.Get(context.Background(), platform.ID); err != nil {
		t.Fatalf("refused withdraw removed the platform: %v", err)
	}
	if _, err := f.apps.Get(context.Background(), older.ID); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("withdrawn app err = %v, want it to stay withdrawn", err)
	}
	got, err := f.apps.Get(context.Background(), live.ID)
	if err != nil {
		t.Fatalf("live app: %v", err)
	}
	if describeApp(got) != liveBefore {
		t.Fatalf("live app changed\n got %s\nwant %s", describeApp(got), liveBefore)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: live.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw live app: %v", err)
	}
	if err := f.platforms.Withdraw(context.Background(), WithdrawRequest{ID: platform.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw empty platform: %v", err)
	}
	if _, err := f.apps.Get(context.Background(), older.ID); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("older app after platform withdraw: %v", err)
	}
	if _, err := f.platforms.Get(context.Background(), platform.ID); !errors.Is(err, wechat.ErrPlatformNotFound) {
		t.Fatalf("withdrawn platform err = %v", err)
	}
}

func TestUnreadyPlatformDoesNotAffectOtherApps(t *testing.T) {
	f := newAuthFixture(t)
	ready := f.holdReady(t, "华东平台", "wxcompappid000001")
	unready, err := f.platforms.Create(context.Background(), CreateRequest{
		Name:            "未就绪平台",
		ComponentAppID:  "wxcompappid0000un",
		ComponentSecret: testComponentSecret,
		Token:           testToken,
		EncodingAESKey:  testEncodingAESKey,
		ActorID:         7,
	})
	if err != nil {
		t.Fatalf("unready: %v", err)
	}
	granted := f.grantMini(t, ready.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	held, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	grantedBefore := describeApp(granted)
	heldBefore := describeApp(held.App)
	f.script(AuthorizationGrant{
		AppID:        "wxminiappid000077",
		Name:         "不该出现",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilityMiniProgramLogin},
		RefreshToken: "authorizer-refresh-ZZZZ",
	})
	link, err := f.apps.StartAuthorization(context.Background(), StartAuthorizationRequest{PlatformID: unready.ID})
	if !errors.Is(err, wechat.ErrPlatformNotReady) || link != nil {
		t.Fatalf("start = (%v, %v)", link, err)
	}
	created, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: unready.ID,
		AuthCode:   "auth-code-unready",
	})
	if !errors.Is(err, wechat.ErrPlatformNotReady) || created != nil {
		t.Fatalf("complete = (%v, %v)", created, err)
	}
	gotGranted, err := f.apps.Get(context.Background(), granted.ID)
	if err != nil {
		t.Fatalf("granted: %v", err)
	}
	gotHeld, err := f.apps.Get(context.Background(), held.App.ID)
	if err != nil {
		t.Fatalf("held: %v", err)
	}
	if describeApp(gotGranted) != grantedBefore || describeApp(gotHeld) != heldBefore {
		t.Fatalf("unready platform changed other apps\ngranted %s\nheld %s", describeApp(gotGranted), describeApp(gotHeld))
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live apps = %d, want the ready platform's app and the direct hold", len(listed))
	}
}

func TestReauthorizationRefusedWhilePlatformUnready(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	app := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	before := describeApp(app)
	if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), platform.ID); err != nil {
		t.Fatalf("release ticket: %v", err)
	}
	f.script(AuthorizationGrant{
		AppID:        app.AppID,
		Name:         app.Name,
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-LATE",
	})
	restored, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-late",
	})
	if !errors.Is(err, wechat.ErrPlatformNotReady) || restored != nil {
		t.Fatalf("reauthorize = (%v, %v)", restored, err)
	}
	got, err := f.apps.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if describeApp(got) != before {
		t.Fatalf("unready reauthorization changed the app\n got %s\nwant %s", describeApp(got), before)
	}
}

func TestAuthorizedPushCreatesAndRestoresTheApp(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxminiappid000099",
		Name:         "推送授权",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilityMiniProgramLogin},
		RefreshToken: "authorizer-refresh-PUSH",
	})
	outcome, err := f.platforms.Receive(context.Background(), encryptedAuthorizedPush(t, platform.ID, platform.ComponentAppID, "authorized", "auth-code-push"))
	if err != nil {
		t.Fatalf("authorized push: %v", err)
	}
	if outcome == nil || outcome.Body != "success" {
		t.Fatalf("outcome = %+v", outcome)
	}
	listed, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].AppID != "wxminiappid000099" || listed[0].PlatformID != platform.ID {
		t.Fatalf("after authorized push = %+v", listed)
	}
	if listed[0].CreateBy != 0 || listed[0].AppSecretMask != "" {
		t.Fatalf("push-created app = %+v", listed[0])
	}
	firstID := listed[0].ID

	f.script(AuthorizationGrant{
		AppID:        "wxminiappid000099",
		Name:         "推送授权",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-PUSH2",
	})
	outcome, err = f.platforms.Receive(context.Background(), encryptedAuthorizedPush(t, platform.ID, platform.ComponentAppID, "updateauthorized", "auth-code-update"))
	if err != nil {
		t.Fatalf("updateauthorized push: %v", err)
	}
	if outcome == nil || outcome.Body != "success" {
		t.Fatalf("outcome = %+v", outcome)
	}
	got, err := f.apps.Get(context.Background(), firstID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != firstID || got.PlatformID != platform.ID || !sameCaps(got.Capabilities, []string{entity.CapabilitySubscribeMessage}) {
		t.Fatalf("restored from push = %+v", got)
	}
	listed, err = f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("updateauthorized inserted another row: %+v", listed)
	}
}

func TestWithdrawnAppIsNotRestoredByALaterGrant(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	first := f.grantMini(t, platform.ID, "wxminiappid000099", []string{entity.CapabilityMiniProgramLogin})
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: first.ID, ActorID: 7}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	f.script(AuthorizationGrant{
		AppID:        first.AppID,
		Name:         "重新授权",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: []string{entity.CapabilitySubscribeMessage},
		RefreshToken: "authorizer-refresh-AGAI",
	})
	second, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-new-row",
	})
	if err != nil {
		t.Fatalf("grant after withdraw: %v", err)
	}
	if second.ID == first.ID || second.AppID != first.AppID || second.Name != "重新授权" {
		t.Fatalf("second = %+v, first id %d", second, first.ID)
	}
	if _, err := f.apps.Get(context.Background(), first.ID); !errors.Is(err, wechat.ErrAppNotFound) {
		t.Fatalf("withdrawn app was restored: %v", err)
	}
}

func (f *authFixture) grantMini(t *testing.T, platformID int64, appID string, caps []string) *entity.App {
	t.Helper()
	f.script(AuthorizationGrant{
		AppID:        appID,
		Name:         "授权小程序",
		AppType:      entity.AppTypeMiniProgram,
		Capabilities: caps,
		RefreshToken: "authorizer-refresh-" + appID,
	})
	created, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platformID,
		AuthCode:   "auth-code-" + appID,
	})
	if err != nil {
		t.Fatalf("grant %s: %v", appID, err)
	}
	return created
}

func encryptedAuthorizedPush(t *testing.T, platformID int64, componentAppID, infoType, code string) PushRequest {
	t.Helper()
	plain := "<xml><AppId><![CDATA[" + componentAppID + "]]></AppId><CreateTime>1413192605</CreateTime><InfoType><![CDATA[" + infoType + "]]></InfoType><AuthorizerAppid><![CDATA[wxauthorizer]]></AuthorizerAppid><AuthorizationCode><![CDATA[" + code + "]]></AuthorizationCode><AuthorizationCodeExpiredTime>1413192605</AuthorizationCodeExpiredTime></xml>"
	return encryptedPush(t, platformID, componentAppID, plain)
}

func sameCaps(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func describeApp(app *entity.App) string {
	if app == nil {
		return "<nil>"
	}
	return strings.Join([]string{
		strings.TrimSpace(app.Name),
		app.AppID,
		app.AppType,
		app.Kind,
		app.Connection,
		strings.Join(app.Capabilities, ","),
		app.AppSecretMask,
		app.TokenMask,
		app.EncodingAESKeyMask,
		app.CallbackAddress,
	}, "|") + "|" + itoa(app.ID) + "|" + itoa(app.PlatformID) + "|" + boolWord(app.Suspended) + "|" + lapseWord(app.Lapsed) + "|" + itoa(app.CreateBy) + "|" + itoa(app.UpdateBy)
}

func boolWord(v bool) string {
	if v {
		return "suspended"
	}
	return "live"
}

func lapseWord(v bool) string {
	if v {
		return "lapsed"
	}
	return "clear"
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
