package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/internal/infra/wechat/crypt"
	"glex/pkg/listq"
)

// memPlatformRepo is the in-memory repository the domain service tests
// stand up. Writes seal secrets with the real cipher and never retain
// plaintext on the row the service reads back.
type memPlatformRepo struct {
	cipher *crypt.Cipher
	rows   map[int64]*memPlatform
	nextID int64
}

type memPlatform struct {
	platform  *entity.Platform
	token     string
	withdrawn bool
}

func newMemPlatformRepo(cipher *crypt.Cipher) *memPlatformRepo {
	return &memPlatformRepo{
		cipher: cipher,
		rows:   map[int64]*memPlatform{},
		nextID: 100,
	}
}

func (r *memPlatformRepo) NextID() int64 { r.nextID++; return r.nextID }

func (r *memPlatformRepo) FindByID(_ context.Context, id int64) (*entity.Platform, error) {
	row, ok := r.rows[id]
	if !ok || row.platform == nil || row.withdrawn {
		return nil, wechat.ErrPlatformNotFound
	}
	cp := *row.platform
	return &cp, nil
}

func (r *memPlatformRepo) List(context.Context, listq.Query) ([]*entity.Platform, error) {
	out := make([]*entity.Platform, 0, len(r.rows))
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		cp := *row.platform
		out = append(out, &cp)
	}
	return out, nil
}

func (r *memPlatformRepo) FindLiveByComponentAppID(_ context.Context, componentAppID string) (*entity.Platform, error) {
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		if row.platform.ComponentAppID == componentAppID {
			cp := *row.platform
			return &cp, nil
		}
	}
	return nil, wechat.ErrPlatformNotFound
}

func (r *memPlatformRepo) LoadSecrets(_ context.Context, id int64) (repository.Secrets, error) {
	row, ok := r.rows[id]
	if !ok {
		return repository.Secrets{}, wechat.ErrPlatformNotFound
	}
	values, err := r.cipher.Decrypt(row.token)
	if err != nil {
		if errors.Is(err, crypt.ErrMasterKeyMissing) {
			return repository.Secrets{}, wechat.ErrMasterKeyMissing
		}
		return repository.Secrets{}, wechat.ErrSecretCrypto
	}
	return repository.Secrets{
		ComponentSecret: values["component_secret"],
		Token:           values["token"],
		EncodingAESKey:  values["encoding_aes_key"],
	}, nil
}

func (r *memPlatformRepo) seal(secrets repository.Secrets) (string, error) {
	token, err := r.cipher.Encrypt(map[string]string{
		"component_secret": secrets.ComponentSecret,
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

func (r *memPlatformRepo) Create(_ context.Context, req repository.SavePlatformRequest) (*entity.Platform, error) {
	token, err := r.seal(req.Secrets)
	if err != nil {
		return nil, err
	}
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		if row.platform.ComponentAppID == req.Platform.ComponentAppID {
			return nil, wechat.ErrPlatformDuplicate
		}
	}
	cp := *req.Platform
	r.rows[cp.ID] = &memPlatform{platform: &cp, token: token}
	out := cp
	return &out, nil
}

func (r *memPlatformRepo) Update(_ context.Context, req repository.SavePlatformRequest) (*entity.Platform, error) {
	row, ok := r.rows[req.Platform.ID]
	if !ok {
		return nil, wechat.ErrPlatformNotFound
	}
	cp := *req.Platform
	if req.ReplaceSecrets {
		token, err := r.seal(req.Secrets)
		if err != nil {
			return nil, err
		}
		row.token = token
	}
	cp.UpdateBy = req.ActorID
	row.platform = &cp
	out := cp
	return &out, nil
}

func (r *memPlatformRepo) Delete(_ context.Context, req repository.DeletePlatformRequest) error {
	row, ok := r.rows[req.ID]
	if !ok || row.withdrawn {
		return wechat.ErrPlatformNotFound
	}
	row.withdrawn = true
	row.platform.UpdateBy = req.ActorID
	return nil
}

const (
	testComponentSecret = "component-secret-AAAA"
	testToken           = "token-value-BBBB"
	testEncodingAESKey  = "encoding-aes-key-CCCC"
)

func TestRegisterReturnsMasksNotPlaintext(t *testing.T) {
	svc := newTestPlatformService(t)
	created, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "华东平台" || created.ComponentAppID != "wxcompappid000001" {
		t.Fatalf("created platform = %+v", created)
	}
	if created.CreateBy != 7 || created.UpdateBy != 7 {
		t.Fatalf("audit = create_by %d update_by %d", created.CreateBy, created.UpdateBy)
	}
	assertMasks(t, created, testComponentSecret, testToken, testEncodingAESKey)

	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertMasks(t, got, testComponentSecret, testToken, testEncodingAESKey)
}

func TestRegisterRejectsIncomplete(t *testing.T) {
	svc := newTestPlatformService(t)
	cases := []CreateRequest{
		{ComponentAppID: "wxcompappid000001", ComponentSecret: testComponentSecret, Token: testToken, EncodingAESKey: testEncodingAESKey},
		{Name: "华东平台", ComponentSecret: testComponentSecret, Token: testToken, EncodingAESKey: testEncodingAESKey},
		{Name: "华东平台", ComponentAppID: "wxcompappid000001", Token: testToken, EncodingAESKey: testEncodingAESKey},
		{Name: "华东平台", ComponentAppID: "wxcompappid000001", ComponentSecret: testComponentSecret, EncodingAESKey: testEncodingAESKey},
		{Name: "华东平台", ComponentAppID: "wxcompappid000001", ComponentSecret: testComponentSecret, Token: testToken},
		{Name: strings.Repeat("名", 65), ComponentAppID: "wxcompappid000001", ComponentSecret: testComponentSecret, Token: testToken, EncodingAESKey: testEncodingAESKey},
		{Name: "华东平台", ComponentAppID: strings.Repeat("a", 65), ComponentSecret: testComponentSecret, Token: testToken, EncodingAESKey: testEncodingAESKey},
		{Name: "华东平台", ComponentAppID: "wxcompappid000001", ComponentSecret: strings.Repeat("s", 257), Token: testToken, EncodingAESKey: testEncodingAESKey},
	}
	for _, in := range cases {
		if _, err := svc.Create(context.Background(), in); !errors.Is(err, wechat.ErrPlatformInvalid) {
			t.Fatalf("create %+v err = %v, want ErrPlatformInvalid", in, err)
		}
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("incomplete register stored %d platforms", len(listed))
	}
}

func TestRenameKeepsComponentAppIDAndMasks(t *testing.T) {
	svc := newTestPlatformService(t)
	created, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := svc.Update(context.Background(), UpdateRequest{
		ID:      created.ID,
		Name:    "华北平台",
		ActorID: 8,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "华北平台" || updated.UpdateBy != 8 || updated.ComponentAppID != created.ComponentAppID {
		t.Fatalf("updated = %+v", updated)
	}
	assertMasks(t, updated, testComponentSecret, testToken, testEncodingAESKey)

	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "华北平台" || got.ComponentAppID != "wxcompappid000001" || got.UpdateBy != 8 {
		t.Fatalf("stored = %+v", got)
	}
	assertMasks(t, got, testComponentSecret, testToken, testEncodingAESKey)

	if _, err := svc.Update(context.Background(), UpdateRequest{ID: created.ID, ActorID: 8}); !errors.Is(err, wechat.ErrPlatformInvalid) {
		t.Fatalf("blank name err = %v, want ErrPlatformInvalid", err)
	}
	got, err = svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get after blank name: %v", err)
	}
	if got.Name != "华北平台" {
		t.Fatalf("blank name changed the stored name to %q", got.Name)
	}
}

func TestReplaceSecretsKeepsBlankOnes(t *testing.T) {
	svc := newTestPlatformService(t)
	created, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const newSecret = "component-secret-ZZZZ"
	updated, err := svc.Update(context.Background(), UpdateRequest{
		ID:              created.ID,
		Name:            "华东平台",
		ComponentSecret: newSecret,
		ActorID:         9,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ComponentAppID != created.ComponentAppID || updated.Name != "华东平台" {
		t.Fatalf("identity changed: %+v", updated)
	}
	assertMasks(t, updated, newSecret, testToken, testEncodingAESKey)
	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertMasks(t, got, newSecret, testToken, testEncodingAESKey)
	if strings.Contains(got.ComponentSecretMask+got.TokenMask+got.EncodingAESKeyMask, newSecret) {
		t.Fatalf("replacement returned plaintext: %+v", got)
	}
}

func TestComponentAppIDIsImmutable(t *testing.T) {
	svc := newTestPlatformService(t)
	created, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.Update(context.Background(), UpdateRequest{
		ID:             created.ID,
		Name:           "改名不成",
		ComponentAppID: "wxcompappid999999",
		ActorID:        8,
	})
	if !errors.Is(err, wechat.ErrPlatformAppIDImmutable) {
		t.Fatalf("err = %v, want ErrPlatformAppIDImmutable", err)
	}
	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ComponentAppID != "wxcompappid000001" || got.Name != "华东平台" {
		t.Fatalf("immutable update changed the row: %+v", got)
	}
	assertMasks(t, got, testComponentSecret, testToken, testEncodingAESKey)

	updated, err := svc.Update(context.Background(), UpdateRequest{
		ID:             created.ID,
		Name:           "华东平台",
		ComponentAppID: created.ComponentAppID,
		ActorID:        8,
	})
	if err != nil {
		t.Fatalf("same app id: %v", err)
	}
	if updated.ComponentAppID != created.ComponentAppID {
		t.Fatalf("app id = %q", updated.ComponentAppID)
	}
}

func TestDuplicateLiveComponentAppIDIsRejected(t *testing.T) {
	svc := newTestPlatformService(t)
	first, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	again := registerInput(8)
	again.Name = "另一条"
	if _, err := svc.Create(context.Background(), again); !errors.Is(err, wechat.ErrPlatformDuplicate) {
		t.Fatalf("err = %v, want ErrPlatformDuplicate", err)
	}
	other := registerInput(8)
	other.ComponentAppID = "wxcompappid000002"
	other.Name = "另一条"
	second, err := svc.Create(context.Background(), other)
	if err != nil {
		t.Fatalf("different app id: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("second platform reused id %d", second.ID)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live platforms = %d, want 2", len(listed))
	}
}

func TestWithdrawOmitsPlatformFromLiveList(t *testing.T) {
	svc := newTestPlatformService(t)
	created, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Withdraw(context.Background(), WithdrawRequest{ID: created.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range listed {
		if p.ID == created.ID {
			t.Fatalf("withdrawn platform still live: %+v", p)
		}
	}
	if _, err := svc.Get(context.Background(), created.ID); !errors.Is(err, wechat.ErrPlatformNotFound) {
		t.Fatalf("get withdrawn err = %v, want ErrPlatformNotFound", err)
	}
	if err := svc.Withdraw(context.Background(), WithdrawRequest{ID: created.ID, ActorID: 9}); !errors.Is(err, wechat.ErrPlatformNotFound) {
		t.Fatalf("second withdraw err = %v, want ErrPlatformNotFound", err)
	}
}

func TestReregisterAfterWithdrawIsANewRow(t *testing.T) {
	svc := newTestPlatformService(t)
	first, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Withdraw(context.Background(), WithdrawRequest{ID: first.ID, ActorID: 7}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	again := registerInput(8)
	again.Name = "重新登记"
	second, err := svc.Create(context.Background(), again)
	if err != nil {
		t.Fatalf("reregister: %v", err)
	}
	if second.ID == first.ID || second.ComponentAppID != first.ComponentAppID || second.Name != "重新登记" {
		t.Fatalf("reregistered = %+v, first id %d", second, first.ID)
	}
	assertMasks(t, second, testComponentSecret, testToken, testEncodingAESKey)
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != second.ID {
		t.Fatalf("live list = %+v, want only the new row", listed)
	}
}

func TestMissingMasterKeyRefusesSecretWrites(t *testing.T) {
	t.Setenv(crypt.EnvMasterKey, "")
	svc := NewPlatformService(newMemPlatformRepo(crypt.New()), slog.New(slog.DiscardHandler))
	created, err := svc.Create(context.Background(), registerInput(7))
	if !errors.Is(err, wechat.ErrMasterKeyMissing) || created != nil {
		t.Fatalf("register = (%v, %v), want master-key refusal and no row", created, err)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("refused register stored %+v", listed)
	}

	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	repo := newMemPlatformRepo(crypt.New())
	svc = NewPlatformService(repo, slog.New(slog.DiscardHandler))
	stored, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create with key: %v", err)
	}
	t.Setenv(crypt.EnvMasterKey, "")
	repo.cipher = crypt.New()
	const freshSecret = "component-secret-NNNN"
	replaced, err := svc.Update(context.Background(), UpdateRequest{
		ID:              stored.ID,
		Name:            "华东平台",
		ComponentSecret: freshSecret,
		ActorID:         9,
	})
	if !errors.Is(err, wechat.ErrMasterKeyMissing) || replaced != nil {
		t.Fatalf("replace = (%v, %v), want master-key refusal", replaced, err)
	}
	got, err := svc.Get(context.Background(), stored.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertMasks(t, got, testComponentSecret, testToken, testEncodingAESKey)
	if strings.Contains(got.ComponentSecretMask+got.TokenMask+got.EncodingAESKeyMask, freshSecret) {
		t.Fatalf("refused replacement stored plaintext: %+v", got)
	}

	renamed, err := svc.Update(context.Background(), UpdateRequest{
		ID:      stored.ID,
		Name:    "只改名称",
		ActorID: 9,
	})
	if err != nil {
		t.Fatalf("rename without master key: %v", err)
	}
	if renamed.Name != "只改名称" {
		t.Fatalf("renamed = %+v", renamed)
	}
	assertMasks(t, renamed, testComponentSecret, testToken, testEncodingAESKey)
}

func registerInput(actorID int64) CreateRequest {
	return CreateRequest{
		Name:            "华东平台",
		ComponentAppID:  "wxcompappid000001",
		ComponentSecret: testComponentSecret,
		Token:           testToken,
		EncodingAESKey:  testEncodingAESKey,
		ActorID:         actorID,
	}
}

func newTestPlatformService(t *testing.T) Platform {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	repo := newMemPlatformRepo(crypt.New())
	return NewPlatformService(repo, slog.New(slog.DiscardHandler))
}

func assertMasks(t *testing.T, p *entity.Platform, secret, token, aesKey string) {
	t.Helper()
	if p == nil {
		t.Fatal("platform is nil")
	}
	want := map[string]string{
		"component secret": maskOf(secret),
		"token":            maskOf(token),
		"encoding aes key": maskOf(aesKey),
	}
	got := map[string]string{
		"component secret": p.ComponentSecretMask,
		"token":            p.TokenMask,
		"encoding aes key": p.EncodingAESKeyMask,
	}
	for name, wantMask := range want {
		if got[name] != wantMask {
			t.Fatalf("%s mask = %q, want %q", name, got[name], wantMask)
		}
	}
	rendered := p.Name + p.ComponentAppID + p.ComponentSecretMask + p.TokenMask + p.EncodingAESKeyMask
	for _, plain := range []string{secret, token, aesKey} {
		if strings.Contains(rendered, plain) {
			t.Fatalf("platform exposes plaintext %q in %+v", plain, p)
		}
	}
}

// maskOf is the operator-visible mask merchant credentials use: the last
// four characters stay visible, and a shorter value collapses to ****.
func maskOf(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}
