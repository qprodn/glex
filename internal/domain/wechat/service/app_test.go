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
	cp := *row.app
	return &cp, nil
}

func (r *memAppRepo) List(context.Context, listq.Query) ([]*entity.App, error) {
	out := make([]*entity.App, 0, len(r.rows))
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		cp := *row.app
		out = append(out, &cp)
	}
	return out, nil
}

func (r *memAppRepo) FindLiveByAppID(_ context.Context, appID string) (*entity.App, error) {
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		if row.app.AppID == appID {
			cp := *row.app
			return &cp, nil
		}
	}
	return nil, wechat.ErrAppNotFound
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
		AppSecret:      values["app_secret"],
		Token:          values["token"],
		EncodingAESKey: values["encoding_aes_key"],
	}, nil
}

func (r *memAppRepo) seal(secrets repository.AppSecrets) (string, error) {
	token, err := r.cipher.Encrypt(map[string]string{
		"app_secret":       secrets.AppSecret,
		"token":            secrets.Token,
		"encoding_aes_key": secrets.EncodingAESKey,
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
	cp := *req.App
	cp.CallbackAddress = ""
	r.rows[cp.ID] = &memApp{app: &cp, token: token}
	out := cp
	return &out, nil
}

func (r *memAppRepo) Update(_ context.Context, req repository.SaveAppRequest) (*entity.App, error) {
	row, ok := r.rows[req.App.ID]
	if !ok || row.withdrawn {
		return nil, wechat.ErrAppNotFound
	}
	cp := *req.App
	cp.CallbackAddress = ""
	if req.ReplaceSecrets {
		token, err := r.seal(req.Secrets)
		if err != nil {
			return nil, err
		}
		row.token = token
	}
	cp.UpdateBy = req.ActorID
	row.app = &cp
	out := cp
	return &out, nil
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
	svc := NewAppService(newMemAppRepo(crypt.New()), slog.New(slog.DiscardHandler))
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
	svc = NewAppService(repo, slog.New(slog.DiscardHandler))
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
	return NewAppService(repo, slog.New(slog.DiscardHandler))
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
