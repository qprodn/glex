package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/pay/crypt"
)

// ---- fakes ----

// fakeMerchantRepo mirrors the production repo boundary: Create/Update
// seal Config whole into ConfigCipher and never store plaintext;
// LoadConfig opens the token (ADR 0018).
type fakeMerchantRepo struct {
	cipher *crypt.Cipher
	m      map[int64]*payentity.MerchantAccount
	nextID int64
	deleted map[int64]bool
}

func newFakeMerchantRepo(cipher *crypt.Cipher) *fakeMerchantRepo {
	return &fakeMerchantRepo{
		cipher:  cipher,
		m:       map[int64]*payentity.MerchantAccount{},
		deleted: map[int64]bool{},
		nextID:  100,
	}
}

func (f *fakeMerchantRepo) FindByID(ctx context.Context, id int64) (*payentity.MerchantAccount, error) {
	if f.deleted[id] {
		return nil, pay.ErrMerchantNotFound
	}
	if m, ok := f.m[id]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, pay.ErrMerchantNotFound
}

func (f *fakeMerchantRepo) List(ctx context.Context, opts ...payrepo.ListOption) ([]*payentity.MerchantAccount, error) {
	return nil, nil
}

func (f *fakeMerchantRepo) LoadConfig(ctx context.Context, m *payentity.MerchantAccount) error {
	stored, ok := f.m[m.ID]
	if !ok {
		return pay.ErrConfigCrypto
	}
	values, err := f.cipher.Decrypt(stored.ConfigCipher)
	if err != nil {
		return err
	}
	m.Config = values
	return nil
}

func (f *fakeMerchantRepo) seal(values map[string]string) (string, error) {
	token, err := f.cipher.Encrypt(values)
	if errors.Is(err, crypt.ErrMasterKeyMissing) {
		// The production repo boundary maps the crypt sentinel to the
		// module's reason-carrying error; the fake mirrors it.
		return "", pay.ErrMasterKeyMissing
	}
	return token, err
}

func (f *fakeMerchantRepo) Create(ctx context.Context, m *payentity.MerchantAccount) (*payentity.MerchantAccount, error) {
	token, err := f.seal(m.Config)
	if err != nil {
		return nil, err
	}
	cp := *m
	cp.ConfigCipher = token
	cp.Config = nil
	f.m[m.ID] = &cp
	return &cp, nil
}

func (f *fakeMerchantRepo) Update(ctx context.Context, m *payentity.MerchantAccount, actorID int64) (*payentity.MerchantAccount, error) {
	if _, ok := f.m[m.ID]; !ok {
		return nil, pay.ErrMerchantNotFound
	}
	token, err := f.seal(m.Config)
	if err != nil {
		return nil, err
	}
	cp := *m
	cp.ConfigCipher = token
	cp.Config = nil
	cp.UpdateBy = actorID
	f.m[m.ID] = &cp
	return &cp, nil
}

func (f *fakeMerchantRepo) UpdateStatus(ctx context.Context, id int64, status payentity.MerchantStatus, actorID int64) error {
	if _, ok := f.m[id]; !ok {
		return pay.ErrMerchantNotFound
	}
	f.m[id].Status = status
	return nil
}

func (f *fakeMerchantRepo) Delete(ctx context.Context, id, actorID int64) error {
	if _, ok := f.m[id]; !ok || f.deleted[id] {
		return pay.ErrMerchantNotFound
	}
	f.deleted[id] = true
	return nil
}

func (f *fakeMerchantRepo) NextID() int64 { f.nextID++; return f.nextID }

type fakeCatalog struct{ schemas []payentity.ChannelSchema }

func (f *fakeCatalog) Schemas() []payentity.ChannelSchema { return f.schemas }

func (f *fakeCatalog) Schema(ch payentity.Channel) (payentity.ChannelSchema, bool) {
	for _, s := range f.schemas {
		if s.Channel == ch {
			return s, true
		}
	}
	return payentity.ChannelSchema{}, false
}

func (f *fakeCatalog) Adopted(ch payentity.Channel) bool {
	_, ok := f.Schema(ch)
	return ok
}

// ValidateConfig applies the schema's core semantics: unknown keys
// rejected, required fields present, values passed through.
func (f *fakeCatalog) ValidateConfig(ch payentity.Channel, values map[string]string) (map[string]string, error) {
	schema, ok := f.Schema(ch)
	if !ok {
		return nil, pay.ErrChannelNotAdopted
	}
	known := map[string]bool{}
	for _, field := range schema.Fields {
		known[field.Name] = true
	}
	cleaned := map[string]string{}
	for name, v := range values {
		if !known[name] {
			return nil, pay.ErrMerchantInvalid
		}
		cleaned[name] = v
	}
	for _, field := range schema.Fields {
		if field.Required && cleaned[field.Name] == "" {
			return nil, pay.ErrMerchantInvalid
		}
	}
	return cleaned, nil
}

func (f *fakeCatalog) MaskConfig(ch payentity.Channel, values map[string]string) map[string]string {
	schema, ok := f.Schema(ch)
	if !ok {
		return nil
	}
	masked := map[string]string{}
	for _, field := range schema.Fields {
		if values[field.Name] != "" {
			masked[field.Name] = "mask:" + field.Name
		}
	}
	return masked
}

type fakeGateways struct {
	probeErr  error
	notAdopte bool
	invalidat []int64
}

func (f *fakeGateways) Resolve(ctx context.Context, m *payentity.MerchantAccount) (ChannelGateway, error) {
	if f.notAdopte {
		return nil, pay.ErrChannelNotAdopted
	}
	return &fakeChannelGateway{probeErr: f.probeErr}, nil
}

func (f *fakeGateways) Invalidate(merchantID int64) { f.invalidat = append(f.invalidat, merchantID) }

type fakeChannelGateway struct{ probeErr error }

func (g *fakeChannelGateway) Channel() payentity.Channel { return payentity.ChannelWechat }

func (g *fakeChannelGateway) Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error) {
	return PrepayResult{}, nil
}

func (g *fakeChannelGateway) Query(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) (ChannelPaymentState, error) {
	return ChannelPaymentState{}, nil
}

func (g *fakeChannelGateway) Close(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) error {
	return nil
}

func (g *fakeChannelGateway) Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}

func (g *fakeChannelGateway) QueryRefund(ctx context.Context, m *payentity.MerchantAccount, refundNo string) (ChannelRefundState, error) {
	return ChannelRefundState{}, nil
}

func (g *fakeChannelGateway) ParseNotification(ctx context.Context, m *payentity.MerchantAccount, headers http.Header, body []byte) (payentity.ChannelNotification, error) {
	return payentity.ChannelNotification{}, nil
}

func (g *fakeChannelGateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error {
	return g.probeErr
}

// ---- fixtures ----

const (
	testAPIv3Key = "a1b2c3d4e5f6g7h8a1b2c3d4e5f6g7h8"
	testPrivKey  = "-----BEGIN PRIVATE KEY-----"
)

func wechatSchema() payentity.ChannelSchema {
	return payentity.ChannelSchema{
		Channel: payentity.ChannelWechat,
		Name:    "wechat",
		Label:   "微信支付",
		Fields: []payentity.SchemaField{
			{Name: "mchid", Label: "商户号", Type: payentity.FieldText, Required: true},
			{Name: "api_v3_key", Label: "APIv3 密钥", Type: payentity.FieldPassword, Required: true, Secret: true},
			{Name: "merchant_private_key", Label: "商户私钥", Type: payentity.FieldTextarea, Required: true, Secret: true},
		},
	}
}

func wechatInput() *CreateInput {
	return &CreateInput{
		Channel: payentity.ChannelWechat,
		Name:    "主店",
		Config: map[string]string{
			"mchid":                "1900000001",
			"api_v3_key":           testAPIv3Key,
			"merchant_private_key": testPrivKey,
		},
	}
}

// newTestMerchantService builds the service over fakes and the real
// cipher (a fixed test key via the environment).
func newTestMerchantService(t *testing.T) (Merchant, *fakeMerchantRepo, *fakeGateways) {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=") // "abcdefghijklmnopqrstuvwxyz123456"
	return newTestMerchantServiceWith(t, crypt.New())
}

func newTestMerchantServiceWith(t *testing.T, cipher *crypt.Cipher) (Merchant, *fakeMerchantRepo, *fakeGateways) {
	t.Helper()
	catalog := &fakeCatalog{schemas: []payentity.ChannelSchema{wechatSchema()}}
	repo := newFakeMerchantRepo(cipher)
	gateways := &fakeGateways{}
	svc := NewMerchantService(repo, catalog, cipher, gateways, slog.New(slog.DiscardHandler))
	return svc, repo, gateways
}

// ---- tests ----

func TestMerchantCreateSealsAndEchoes(t *testing.T) {
	svc, repo, _ := newTestMerchantService(t)
	m, err := svc.Create(context.Background(), wechatInput(), 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.ID == 0 || m.Status != payentity.MerchantEnabled || m.CreateBy != 7 || m.UpdateBy != 7 {
		t.Fatalf("created merchant = %+v", m)
	}
	if m.ConfigMasked["api_v3_key"] != "mask:api_v3_key" {
		t.Fatalf("masked echo = %v", m.ConfigMasked)
	}
	if len(m.ConfigDigest) != 64 {
		t.Fatalf("digest length = %d", len(m.ConfigDigest))
	}
	// The stored row carries only the sealed token — no plaintext column
	// exists (ADR 0018).
	stored := repo.m[m.ID]
	if stored.ConfigCipher == "" || stored.Config != nil {
		t.Fatalf("stored row leaks plaintext: cipher=%q config=%v", stored.ConfigCipher, stored.Config)
	}
	// LoadConfig (the update/probe path) decrypts back.
	if err := repo.LoadConfig(context.Background(), stored); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if stored.Config["api_v3_key"] != testAPIv3Key {
		t.Fatalf("decrypted api_v3_key = %q", stored.Config["api_v3_key"])
	}
}

func TestMerchantCreateRejectsInvalidInputs(t *testing.T) {
	svc, _, _ := newTestMerchantService(t)
	in := wechatInput()
	in.Channel = payentity.ChannelStripe
	if _, err := svc.Create(context.Background(), in, 7); !errors.Is(err, pay.ErrChannelNotAdopted) {
		t.Fatalf("err = %v, want ErrChannelNotAdopted", err)
	}
	in = wechatInput()
	in.Name = ""
	if _, err := svc.Create(context.Background(), in, 7); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
	in = wechatInput()
	delete(in.Config, "api_v3_key")
	if _, err := svc.Create(context.Background(), in, 7); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
	in = wechatInput()
	in.Config["bogus"] = "x"
	if _, err := svc.Create(context.Background(), in, 7); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
}

func TestMerchantUpdateKeepsBlankSecrets(t *testing.T) {
	svc, repo, gateways := newTestMerchantService(t)
	m, err := svc.Create(context.Background(), wechatInput(), 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gateways.invalidat = nil
	updated, err := svc.Update(context.Background(), &UpdateInput{
		ID:   m.ID,
		Name: "分店",
		Config: map[string]string{
			"mchid":                "1900000002",
			"api_v3_key":           "", // blank secret → keep
			"merchant_private_key": "NEW-KEY",
		},
	}, 8)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "分店" || updated.UpdateBy != 8 {
		t.Fatalf("updated merchant = %+v", updated)
	}
	stored, err := svc.Get(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Display never decrypts; the assertion inspects plaintext via the
	// repo boundary's explicit LoadConfig.
	if err := repo.LoadConfig(context.Background(), stored); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if stored.Config["api_v3_key"] != testAPIv3Key {
		t.Fatalf("blank secret did not keep its value: %q", stored.Config["api_v3_key"])
	}
	if stored.Config["merchant_private_key"] != "NEW-KEY" {
		t.Fatalf("submitted secret not replaced: %q", stored.Config["merchant_private_key"])
	}
	if stored.Config["mchid"] != "1900000002" {
		t.Fatalf("plain field not replaced: %q", stored.Config["mchid"])
	}
	if len(gateways.invalidat) != 1 || gateways.invalidat[0] != m.ID {
		t.Fatalf("invalidated = %v, want [%d]", gateways.invalidat, m.ID)
	}
}

func TestMerchantChangeStatusAndWithdrawInvalidate(t *testing.T) {
	svc, _, gateways := newTestMerchantService(t)
	m, err := svc.Create(context.Background(), wechatInput(), 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gateways.invalidat = nil
	got, err := svc.ChangeStatus(context.Background(), m.ID, payentity.MerchantDisabled, 7)
	if err != nil {
		t.Fatalf("change status: %v", err)
	}
	if got.Status != payentity.MerchantDisabled {
		t.Fatalf("status = %d", got.Status)
	}
	if _, err := svc.ChangeStatus(context.Background(), m.ID, payentity.MerchantStatus(9), 7); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
	if err := svc.Withdraw(context.Background(), m.ID, 7); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := svc.Get(context.Background(), m.ID); !errors.Is(err, pay.ErrMerchantNotFound) {
		t.Fatalf("err = %v, want ErrMerchantNotFound", err)
	}
	if len(gateways.invalidat) != 2 {
		t.Fatalf("invalidated %d times, want 2 (status + withdraw)", len(gateways.invalidat))
	}
}

func TestMerchantProbeOutcomes(t *testing.T) {
	svc, _, gateways := newTestMerchantService(t)
	m, err := svc.Create(context.Background(), wechatInput(), 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	outcome, err := svc.Probe(context.Background(), m.ID)
	if err != nil || !outcome.OK || outcome.Message == "" {
		t.Fatalf("probe = %+v, err = %v", outcome, err)
	}
	gateways.probeErr = errors.New("platform cert pull refused")
	outcome, err = svc.Probe(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("probe surfaced a gateway failure as transport error: %v", err)
	}
	if outcome.OK || outcome.Message == "" {
		t.Fatalf("probe outcome = %+v, want ok=false with message", outcome)
	}
	gateways.notAdopte = true
	if _, err := svc.Probe(context.Background(), m.ID); !errors.Is(err, pay.ErrChannelNotAdopted) {
		t.Fatalf("err = %v, want ErrChannelNotAdopted", err)
	}
}

func TestMerchantCreateFailsWithoutMasterKey(t *testing.T) {
	// No PAY_MASTER_KEY in the environment: the cipher is inert and every
	// merchant write refuses (ADR 0018) while unrelated paths are
	// unaffected.
	svc, _, _ := newTestMerchantServiceWith(t, crypt.New())
	if _, err := svc.Create(context.Background(), wechatInput(), 7); !errors.Is(err, pay.ErrMasterKeyMissing) {
		t.Fatalf("err = %v, want ErrMasterKeyMissing", err)
	}
}
