package service

import (
	"context"
	"encoding/xml"
	"errors"
	"log/slog"
	"strconv"
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
	ticket    string
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

func (r *memPlatformRepo) view(row *memPlatform) *entity.Platform {
	cp := *row.platform
	cp.Ready = row.token != "" && row.ticket != ""
	cp.CallbackURL = ""
	return &cp
}

func (r *memPlatformRepo) FindByID(_ context.Context, id int64) (*entity.Platform, error) {
	row, ok := r.rows[id]
	if !ok || row.platform == nil || row.withdrawn {
		return nil, wechat.ErrPlatformNotFound
	}
	return r.view(row), nil
}

func (r *memPlatformRepo) List(context.Context, listq.Query) ([]*entity.Platform, error) {
	out := make([]*entity.Platform, 0, len(r.rows))
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		out = append(out, r.view(row))
	}
	return out, nil
}

func (r *memPlatformRepo) FindLiveByComponentAppID(_ context.Context, componentAppID string) (*entity.Platform, error) {
	for _, row := range r.rows {
		if row.withdrawn {
			continue
		}
		if row.platform.ComponentAppID == componentAppID {
			return r.view(row), nil
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

func (r *memPlatformRepo) sealValues(values map[string]string) (string, error) {
	token, err := r.cipher.Encrypt(values)
	if errors.Is(err, crypt.ErrMasterKeyMissing) {
		return "", wechat.ErrMasterKeyMissing
	}
	if err != nil {
		return "", wechat.ErrSecretCrypto
	}
	return token, nil
}

func (r *memPlatformRepo) seal(secrets repository.Secrets) (string, error) {
	return r.sealValues(map[string]string{
		"component_secret": secrets.ComponentSecret,
		"token":            secrets.Token,
		"encoding_aes_key": secrets.EncodingAESKey,
	})
}

func (r *memPlatformRepo) LoadVerifyTicket(_ context.Context, id int64) (string, error) {
	row, ok := r.rows[id]
	if !ok || row.withdrawn {
		return "", wechat.ErrPlatformNotFound
	}
	if row.ticket == "" {
		return "", nil
	}
	values, err := r.cipher.Decrypt(row.ticket)
	if err != nil {
		if errors.Is(err, crypt.ErrMasterKeyMissing) {
			return "", wechat.ErrMasterKeyMissing
		}
		return "", wechat.ErrSecretCrypto
	}
	return values["verify_ticket"], nil
}

func (r *memPlatformRepo) HoldVerifyTicket(_ context.Context, req repository.HoldVerifyTicketRequest) (*entity.Platform, error) {
	row, ok := r.rows[req.ID]
	if !ok || row.withdrawn {
		return nil, wechat.ErrPlatformNotFound
	}
	sealed, err := r.sealValues(map[string]string{"verify_ticket": req.Ticket})
	if err != nil {
		return nil, err
	}
	row.ticket = sealed
	row.platform.UpdateBy = req.ActorID
	return r.view(row), nil
}

func (r *memPlatformRepo) ReleaseVerifyTicket(_ context.Context, req repository.ReleaseVerifyTicketRequest) (*entity.Platform, error) {
	row, ok := r.rows[req.ID]
	if !ok || row.withdrawn {
		return nil, wechat.ErrPlatformNotFound
	}
	row.ticket = ""
	row.platform.UpdateBy = req.ActorID
	return r.view(row), nil
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
	row := &memPlatform{platform: &cp, token: token}
	r.rows[cp.ID] = row
	return r.view(row), nil
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
	return r.view(row), nil
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
	testCallbackBase    = "https://wx.example.com"
	testPushToken       = "push-token-value-BBBB"
	// 43 characters, the length WeChat requires of an EncodingAESKey.
	testPushAESKey = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
)

func TestEachPlatformShowsItsOwnCallback(t *testing.T) {
	svc := newTestPlatformService(t)
	first, err := svc.Create(context.Background(), registerInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := registerInput(8)
	other.Name = "华北平台"
	other.ComponentAppID = "wxcompappid000002"
	second, err := svc.Create(context.Background(), other)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	wantFirst := testCallbackBase + "/wechat/platform/" + strconv.FormatInt(first.ID, 10) + "/callback"
	wantSecond := testCallbackBase + "/wechat/platform/" + strconv.FormatInt(second.ID, 10) + "/callback"
	if first.CallbackURL != wantFirst || second.CallbackURL != wantSecond {
		t.Fatalf("callbacks = %q and %q, want %q and %q", first.CallbackURL, second.CallbackURL, wantFirst, wantSecond)
	}
	if first.Ready || second.Ready {
		t.Fatalf("registered platforms are ready before a verify ticket: %+v %+v", first, second)
	}
	got, err := svc.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CallbackURL != wantFirst || got.Ready {
		t.Fatalf("stored view = callback %q ready %v", got.CallbackURL, got.Ready)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := map[int64]string{}
	for _, p := range listed {
		seen[p.ID] = p.CallbackURL
		if p.Ready {
			t.Fatalf("listed platform ready before a ticket: %+v", p)
		}
	}
	if seen[first.ID] != wantFirst || seen[second.ID] != wantSecond {
		t.Fatalf("listed callbacks = %v", seen)
	}
}

func TestEncryptedVerifyTicketReadiesOnlyThatPlatform(t *testing.T) {
	svc := newTestPlatformService(t)
	first, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := pushRegisterInput(8)
	other.Name = "华北平台"
	other.ComponentAppID = "wxcompappid000002"
	second, err := svc.Create(context.Background(), other)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	const ticket = "ticket-PLAINTEXT-one"
	reply, err := svc.Receive(context.Background(), encryptedTicketPush(t, first.ID, first.ComponentAppID, ticket))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if reply.Body != "success" {
		t.Fatalf("reply = %q, want success", reply.Body)
	}
	got, err := svc.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Ready {
		t.Fatal("verify ticket left the platform unready")
	}
	if got.UpdateBy != 0 || got.CreateBy != 7 {
		t.Fatalf("audit = create_by %d update_by %d, want create_by 7 update_by 0", got.CreateBy, got.UpdateBy)
	}
	if strings.Contains(operatorView(got), ticket) {
		t.Fatalf("verify ticket is visible on the platform: %s", operatorView(got))
	}
	assertMasks(t, got, testComponentSecret, testPushToken, testPushAESKey)
	untouched, err := svc.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	if untouched.Ready || untouched.Name != "华北平台" || untouched.UpdateBy != 8 || untouched.ComponentAppID != "wxcompappid000002" {
		t.Fatalf("other platform changed: %s", operatorView(untouched))
	}
	assertMasks(t, untouched, testComponentSecret, testPushToken, testPushAESKey)
}

func TestLaterVerifyTicketReplacesTheHeldOne(t *testing.T) {
	api := &scriptedOpenAPI{acceptTicket: "ticket-PLAINTEXT-two"}
	svc := newScriptedPlatform(t, api)
	first, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := pushRegisterInput(8)
	other.Name = "华北平台"
	other.ComponentAppID = "wxcompappid000002"
	second, err := svc.Create(context.Background(), other)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, first.ID, first.ComponentAppID, "ticket-PLAINTEXT-one")); err != nil {
		t.Fatalf("first ticket: %v", err)
	}
	failed, err := svc.Probe(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("probe with the ticket being replaced: %v", err)
	}
	if failed.OK {
		t.Fatal("connectivity test accepted the ticket a later push should have replaced")
	}
	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, first.ID, first.ComponentAppID, "ticket-PLAINTEXT-two")); err != nil {
		t.Fatalf("second ticket: %v", err)
	}
	before, err := svc.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	passed, err := svc.Probe(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !passed.OK {
		t.Fatalf("connectivity test rejected the ticket the platform now holds: %+v", passed)
	}
	after, err := svc.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get after probe: %v", err)
	}
	if !after.Ready || operatorView(after) != operatorView(before) || after.UpdateBy != 0 || after.CreateBy != 7 {
		t.Fatalf("probe or replacement changed the platform: before %s update %d, after %s update %d ready %v", operatorView(before), before.UpdateBy, operatorView(after), after.UpdateBy, after.Ready)
	}
	if strings.Contains(operatorView(after), "ticket-PLAINTEXT-one") || strings.Contains(operatorView(after), "ticket-PLAINTEXT-two") {
		t.Fatalf("verify ticket is visible: %s", operatorView(after))
	}
	untouched, err := svc.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	if untouched.Ready || untouched.UpdateBy != 8 {
		t.Fatalf("other platform changed: %s update %d", operatorView(untouched), untouched.UpdateBy)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live platforms = %d, want the two registrations and no app", len(listed))
	}
}

func TestLosingVerifyTicketUnreadiesPlatform(t *testing.T) {
	svc := newTestPlatformService(t)
	first, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := pushRegisterInput(8)
	other.Name = "华北平台"
	other.ComponentAppID = "wxcompappid000002"
	second, err := svc.Create(context.Background(), other)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, second.ID, second.ComponentAppID, "ticket-PLAINTEXT-other")); err != nil {
		t.Fatalf("other ticket: %v", err)
	}
	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, first.ID, first.ComponentAppID, "ticket-PLAINTEXT-one")); err != nil {
		t.Fatalf("ticket: %v", err)
	}
	lost, err := svc.ReleaseVerifyTicket(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if lost.Ready || lost.Name != "华东平台" || lost.ComponentAppID != first.ComponentAppID || lost.UpdateBy != 0 || lost.CreateBy != 7 {
		t.Fatalf("after losing the ticket: %s ready %v update %d", operatorView(lost), lost.Ready, lost.UpdateBy)
	}
	assertMasks(t, lost, testComponentSecret, testPushToken, testPushAESKey)
	if strings.Contains(operatorView(lost), "ticket-PLAINTEXT-one") {
		t.Fatalf("released ticket is visible: %s", operatorView(lost))
	}
	got, err := svc.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Ready || got.CallbackURL != first.CallbackURL {
		t.Fatalf("stored = ready %v callback %q", got.Ready, got.CallbackURL)
	}
	untouched, err := svc.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	if !untouched.Ready || untouched.Name != "华北平台" || untouched.UpdateBy != 0 {
		t.Fatalf("other platform changed: %s ready %v update %d", operatorView(untouched), untouched.Ready, untouched.UpdateBy)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live platforms = %d, want the two registrations and no app", len(listed))
	}
	for _, p := range listed {
		if p.ID == first.ID && p.Ready {
			t.Fatalf("list still shows the platform ready: %+v", p)
		}
	}
}

func TestPlatformProbeFailsUntilReadyAndLeavesPlatformsUnchanged(t *testing.T) {
	api := &scriptedOpenAPI{}
	svc, repo := newScriptedPlatformRepo(t, api)
	created, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := mustGet(t, svc, created.ID)
	failed, err := svc.Probe(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("probe before a ticket: %v", err)
	}
	if failed.OK {
		t.Fatal("connectivity test passed with no verify ticket")
	}
	if len(api.probes) != 0 {
		t.Fatal("connectivity test called WeChat without a verify ticket")
	}
	after := mustGet(t, svc, created.ID)
	if after.Ready || operatorView(after) != operatorView(before) || after.UpdateBy != before.UpdateBy {
		t.Fatalf("failed probe changed the platform: %s update %d -> %s update %d", operatorView(before), before.UpdateBy, operatorView(after), after.UpdateBy)
	}

	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, created.ID, created.ComponentAppID, "ticket-PLAINTEXT-one")); err != nil {
		t.Fatalf("ticket: %v", err)
	}
	sealed, err := repo.seal(repository.Secrets{Token: testPushToken, EncodingAESKey: testPushAESKey})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	repo.rows[created.ID].token = sealed
	incomplete := mustGet(t, svc, created.ID)
	failed, err = svc.Probe(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("probe with a missing component secret: %v", err)
	}
	if failed.OK {
		t.Fatal("connectivity test passed with a missing component secret")
	}
	if len(api.probes) != 0 {
		t.Fatal("connectivity test called WeChat with incomplete credentials")
	}
	still := mustGet(t, svc, created.ID)
	if still.Ready != incomplete.Ready || operatorView(still) != operatorView(incomplete) || still.UpdateBy != incomplete.UpdateBy {
		t.Fatalf("failed probe changed the platform: %s -> %s", operatorView(incomplete), operatorView(still))
	}

	restored, err := repo.seal(repository.Secrets{
		ComponentSecret: testComponentSecret,
		Token:           testPushToken,
		EncodingAESKey:  testPushAESKey,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	repo.rows[created.ID].token = restored
	ready := mustGet(t, svc, created.ID)
	if !ready.Ready {
		t.Fatal("restored secrets left the platform unready")
	}
	passed, err := svc.Probe(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !passed.OK {
		t.Fatalf("ready platform probe = %+v", passed)
	}
	done := mustGet(t, svc, created.ID)
	if !done.Ready || operatorView(done) != operatorView(ready) || done.UpdateBy != ready.UpdateBy {
		t.Fatalf("successful probe changed the platform: %s update %d -> %s update %d", operatorView(ready), ready.UpdateBy, operatorView(done), done.UpdateBy)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("live platforms = %+v, want the one registration and no app", listed)
	}
}

func TestFullNetworkReleaseTextIsAnsweredWithoutCreatingRecords(t *testing.T) {
	api := &scriptedOpenAPI{}
	svc := newScriptedPlatform(t, api)
	created, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := mustGet(t, svc, created.ID)
	reply, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, fullNetworkText("gh_3c884a361561", "oTestFanOpenId0001", "TESTCOMPONENT_MSG_TYPE_TEXT")))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	plain := decryptReply(t, created.ComponentAppID, reply.Body)
	if !strings.Contains(plain, "TESTCOMPONENT_MSG_TYPE_TEXT_callback") || !strings.Contains(plain, "oTestFanOpenId0001") || !strings.Contains(plain, "gh_3c884a361561") {
		t.Fatalf("full-network reply = %s", plain)
	}
	if strings.Contains(plain, "TESTCOMPONENT_MSG_TYPE_TEXT_callback") && strings.Contains(plain, "<Content><![CDATA[TESTCOMPONENT_MSG_TYPE_TEXT]]>") {
		t.Fatalf("reply echoed the test content instead of the published callback: %s", plain)
	}
	after := mustGet(t, svc, created.ID)
	if after.Ready || operatorView(after) != operatorView(before) || after.UpdateBy != before.UpdateBy {
		t.Fatalf("test traffic changed the platform: %s update %d -> %s ready %v update %d", operatorView(before), before.UpdateBy, operatorView(after), after.Ready, after.UpdateBy)
	}
	if len(api.replies) != 0 || len(api.probes) != 0 {
		t.Fatal("text callback created a WeChat call instead of a passive reply")
	}
	ordinary, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, fullNetworkText("gh_not_a_test_account", "oSomeone", "TESTCOMPONENT_MSG_TYPE_TEXT")))
	if err != nil {
		t.Fatalf("ordinary text: %v", err)
	}
	if ordinary.Body != "success" {
		t.Fatalf("ordinary text reply = %q, want success", ordinary.Body)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Ready {
		t.Fatalf("live platforms = %+v, want the one unready registration and no app", listed)
	}
}

func TestFullNetworkReleaseEventIsAnsweredWithoutCreatingRecords(t *testing.T) {
	api := &scriptedOpenAPI{}
	svc := newScriptedPlatform(t, api)
	created, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := mustGet(t, svc, created.ID)
	event := "<xml><ToUserName><![CDATA[gh_3c884a361561]]></ToUserName><FromUserName><![CDATA[oTestFanOpenId0001]]></FromUserName><CreateTime>1413192605</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[subscribe]]></Event></xml>"
	reply, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, event))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if reply.Body == "success" {
		t.Fatal("full-network event fell through to a plain success body")
	}
	plain := decryptReply(t, created.ComponentAppID, reply.Body)
	if !strings.Contains(plain, "TESTCOMPONENT_MSG_TYPE_TEXT_callback") || !strings.Contains(plain, "oTestFanOpenId0001") || !strings.Contains(plain, "gh_3c884a361561") {
		t.Fatalf("full-network event reply = %s", plain)
	}
	after := mustGet(t, svc, created.ID)
	if after.Ready || operatorView(after) != operatorView(before) || after.UpdateBy != before.UpdateBy {
		t.Fatalf("event traffic changed the platform: %s -> %s", operatorView(before), operatorView(after))
	}
	if len(api.replies) != 0 || len(api.probes) != 0 || len(api.completes) != 0 || len(api.starts) != 0 {
		t.Fatal("full-network event created a WeChat call")
	}
	ordinary, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, "<xml><ToUserName><![CDATA[gh_not_a_test_account]]></ToUserName><FromUserName><![CDATA[oSomeone]]></FromUserName><CreateTime>1413192605</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[subscribe]]></Event></xml>"))
	if err != nil {
		t.Fatalf("ordinary event: %v", err)
	}
	if ordinary.Body != "success" {
		t.Fatalf("ordinary event reply = %q, want success", ordinary.Body)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Ready {
		t.Fatalf("live platforms = %+v, want the one unready registration and no app", listed)
	}
}

func TestFullNetworkReleaseQueryAuthCodeIsAnsweredWithoutCreatingRecords(t *testing.T) {
	api := &scriptedOpenAPI{}
	svc := newScriptedPlatform(t, api)
	created, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const ticket = "ticket-PLAINTEXT-one"
	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, created.ID, created.ComponentAppID, ticket)); err != nil {
		t.Fatalf("ticket: %v", err)
	}
	before := mustGet(t, svc, created.ID)
	reply, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, fullNetworkText("gh_8dad206e9538", "oMiniFan0001", "QUERY_AUTH_CODE:auth-code-42")))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if reply.Body != "" {
		t.Fatalf("query-auth reply = %q, want the published empty answer", reply.Body)
	}
	if len(api.replies) != 1 {
		t.Fatalf("customer-service replies = %d, want the published from-api message", len(api.replies))
	}
	got := api.replies[0]
	if got.QueryAuthCode != "auth-code-42" || got.Content != "auth-code-42_from_api" || got.ToUser != "oMiniFan0001" || got.ComponentAppID != created.ComponentAppID || got.VerifyTicket != ticket {
		t.Fatalf("full-network reply handed to WeChat = %+v", got)
	}
	if strings.Contains(operatorView(before), ticket) {
		t.Fatalf("ticket visible before the test push: %s", operatorView(before))
	}
	after := mustGet(t, svc, created.ID)
	if !after.Ready || operatorView(after) != operatorView(before) || after.UpdateBy != before.UpdateBy {
		t.Fatalf("query-auth traffic changed the platform: %s -> %s", operatorView(before), operatorView(after))
	}
	api.replyErr = errors.New("wechat unavailable")
	api.replies = nil
	again, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, fullNetworkText("gh_3c884a361561", "oOfficialFan", "QUERY_AUTH_CODE:auth-code-77")))
	if err != nil {
		t.Fatalf("receive while WeChat is unavailable: %v", err)
	}
	if again.Body != "" || len(api.replies) != 1 || api.replies[0].Content != "auth-code-77_from_api" {
		t.Fatalf("unavailable follow-up reply = %q calls = %+v", again.Body, api.replies)
	}
	still := mustGet(t, svc, created.ID)
	if operatorView(still) != operatorView(before) || !still.Ready {
		t.Fatalf("failed follow-up changed the platform: %s", operatorView(still))
	}
	api.replies = nil
	ordinary, err := svc.Receive(context.Background(), encryptedMessagePush(t, created.ID, created.ComponentAppID, fullNetworkText("gh_not_a_test_account", "oSomeone", "QUERY_AUTH_CODE:auth-code-42")))
	if err != nil {
		t.Fatalf("ordinary: %v", err)
	}
	if ordinary.Body != "success" || len(api.replies) != 0 {
		t.Fatalf("ordinary query-auth reply = %q calls = %d", ordinary.Body, len(api.replies))
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("live platforms = %+v, want the one registration and no app or dispatch", listed)
	}
}

func TestPlaintextCompatibleAndBadSignaturePushesAreRejected(t *testing.T) {
	const ticket = "ticket-PLAINTEXT-one"
	api := &scriptedOpenAPI{acceptTicket: ticket}
	svc := newScriptedPlatform(t, api)
	created, err := svc.Create(context.Background(), pushRegisterInput(7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := pushRegisterInput(8)
	other.Name = "华北平台"
	other.ComponentAppID = "wxcompappid000002"
	second, err := svc.Create(context.Background(), other)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := svc.Receive(context.Background(), encryptedTicketPush(t, created.ID, created.ComponentAppID, ticket)); err != nil {
		t.Fatalf("ticket: %v", err)
	}
	before := mustGet(t, svc, created.ID)
	if !before.Ready {
		t.Fatal("setup ticket did not ready the platform")
	}
	rejects := []PushRequest{
		plaintextPush(created.ID),
		compatiblePush(t, created.ID, created.ComponentAppID),
		badSignaturePush(t, created.ID, created.ComponentAppID),
	}
	for _, req := range rejects {
		if _, err := svc.Receive(context.Background(), req); !errors.Is(err, wechat.ErrPushRejected) {
			t.Fatalf("receive err = %v, want ErrPushRejected", err)
		}
		got := mustGet(t, svc, created.ID)
		if !got.Ready || operatorView(got) != operatorView(before) || got.UpdateBy != before.UpdateBy || got.CreateBy != before.CreateBy {
			t.Fatalf("rejected push changed the platform: %s update %d -> %s ready %v update %d", operatorView(before), before.UpdateBy, operatorView(got), got.Ready, got.UpdateBy)
		}
		passed, err := svc.Probe(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !passed.OK {
			t.Fatalf("rejected push replaced the held ticket: %+v", passed)
		}
	}
	untouched := mustGet(t, svc, second.ID)
	if untouched.Ready || untouched.Name != "华北平台" || untouched.UpdateBy != 8 {
		t.Fatalf("other platform changed: %s ready %v update %d", operatorView(untouched), untouched.Ready, untouched.UpdateBy)
	}
	listed, err := svc.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("live platforms = %d, want the two registrations and no app", len(listed))
	}
}

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
	svc := NewPlatformService(newMemPlatformRepo(crypt.New()), nil, nil, testCallbackBase, nil, slog.New(slog.DiscardHandler))
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
	svc = NewPlatformService(repo, nil, nil, testCallbackBase, nil, slog.New(slog.DiscardHandler))
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

func pushRegisterInput(actorID int64) CreateRequest {
	in := registerInput(actorID)
	in.Token = testPushToken
	in.EncodingAESKey = testPushAESKey
	return in
}

func encryptedTicketPush(t *testing.T, platformID int64, componentAppID, ticket string) PushRequest {
	t.Helper()
	plain := "<xml><AppId><![CDATA[" + componentAppID + "]]></AppId><CreateTime>1413192605</CreateTime><InfoType><![CDATA[component_verify_ticket]]></InfoType><ComponentVerifyTicket><![CDATA[" + ticket + "]]></ComponentVerifyTicket></xml>"
	return encryptedPush(t, platformID, componentAppID, plain)
}

func fullNetworkText(toUser, fromUser, content string) string {
	return "<xml><ToUserName><![CDATA[" + toUser + "]]></ToUserName><FromUserName><![CDATA[" + fromUser + "]]></FromUserName><CreateTime>1413192605</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[" + content + "]]></Content><MsgId>1234567890123456</MsgId></xml>"
}

func encryptedMessagePush(t *testing.T, platformID int64, componentAppID, plain string) PushRequest {
	t.Helper()
	req := encryptedPush(t, platformID, componentAppID, plain)
	enc := encryptField(t, req.Body)
	req.Body = []byte("<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><Encrypt><![CDATA[" + enc + "]]></Encrypt></xml>")
	return req
}

func encryptField(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Encrypt string `xml:"Encrypt"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	return envelope.Encrypt
}

func decryptReply(t *testing.T, componentAppID, body string) string {
	t.Helper()
	plain, err := (wxCrypto{}).decrypt(testPushAESKey, encryptField(t, []byte(body)), componentAppID)
	if err != nil {
		t.Fatalf("decrypt reply: %v", err)
	}
	return string(plain)
}

func plaintextPush(platformID int64) PushRequest {
	return PushRequest{
		PlatformID:  platformID,
		Timestamp:   "1715943329",
		Nonce:       "1590219412",
		EncryptType: "",
		Signature:   msgSignature(testPushToken, "1715943329", "1590219412", ""),
		Body:        []byte(fullNetworkText("gh_3c884a361561", "oSomeone", "hello")),
	}
}

func compatiblePush(t *testing.T, platformID int64, componentAppID string) PushRequest {
	t.Helper()
	sealed := encryptedPush(t, platformID, componentAppID, fullNetworkText("gh_3c884a361561", "oSomeone", "hello"))
	enc := encryptField(t, sealed.Body)
	sealed.Body = []byte("<xml><ToUserName><![CDATA[gh_3c884a361561]]></ToUserName><FromUserName><![CDATA[oSomeone]]></FromUserName><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[hello]]></Content><Encrypt><![CDATA[" + enc + "]]></Encrypt></xml>")
	return sealed
}

func badSignaturePush(t *testing.T, platformID int64, componentAppID string) PushRequest {
	t.Helper()
	req := encryptedTicketPush(t, platformID, componentAppID, "ticket-PLAINTEXT-forged")
	req.MsgSignature = "0000000000000000000000000000000000000000"
	return req
}

func encryptedPush(t *testing.T, platformID int64, componentAppID, plain string) PushRequest {
	t.Helper()
	const timestamp = "1715943329"
	const nonce = "1590219412"
	enc, err := (wxCrypto{}).encrypt(testPushAESKey, componentAppID, []byte(plain))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	body := []byte("<xml><AppId><![CDATA[" + componentAppID + "]]></AppId><Encrypt><![CDATA[" + enc + "]]></Encrypt></xml>")
	return PushRequest{
		PlatformID:   platformID,
		Timestamp:    timestamp,
		Nonce:        nonce,
		EncryptType:  "aes",
		MsgSignature: msgSignature(testPushToken, timestamp, nonce, enc),
		Body:         body,
	}
}

func operatorView(p *entity.Platform) string {
	if p == nil {
		return "<nil>"
	}
	return p.Name + " " + p.ComponentAppID + " " + p.ComponentSecretMask + " " + p.TokenMask + " " + p.EncodingAESKeyMask + " " + p.CallbackURL
}

type scriptedOpenAPI struct {
	probeErr       error
	acceptTicket   string
	probes         []ProbePlatformRequest
	replies        []FullNetworkReply
	replyErr       error
	authLink       string
	authErr        error
	starts         []OpenAuthorizationStart
	grant          *AuthorizationGrant
	grantErr       error
	completes      []OpenAuthorizationCode
	appProbe       ProbeAppResult
	appProbeErr    error
	appProbes      []ProbeAppRequest
	adopt          AdoptLibraryTemplateResult
	adoptErr       error
	adopts         []AdoptLibraryTemplateRequest
	send           SendMessageResult
	sendErr        error
	sends          []SendMessageRequest
	webGrant       *WebAuthorizationExchange
	webGrantErr    error
	webExchanges   []ExchangeWebAuthorizationRequest
	menuPublish    PublishMenuResult
	menuPublishErr error
	menuPublishes  []PublishMenuCall
	miniLogin      MiniProgramLoginResult
	miniLoginErr   error
	miniLogins     []MiniProgramLoginCall
}

func (s *scriptedOpenAPI) PublishMenu(_ context.Context, req PublishMenuCall) (PublishMenuResult, error) {
	if s == nil {
		return PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}, nil
	}
	s.menuPublishes = append(s.menuPublishes, req)
	if s.menuPublishErr != nil {
		return PublishMenuResult{}, s.menuPublishErr
	}
	result := s.menuPublish
	if result.Class == CallSuccess && result.Message == "" {
		result.Message = "已替换默认菜单"
	}
	return result, nil
}

func (s *scriptedOpenAPI) SendMessage(_ context.Context, req SendMessageRequest) (SendMessageResult, error) {
	if s == nil {
		return SendMessageResult{Class: CallSuccess}, nil
	}
	s.sends = append(s.sends, req)
	if s.sendErr != nil {
		return SendMessageResult{}, s.sendErr
	}
	return s.send, nil
}

func (s *scriptedOpenAPI) AdoptLibraryTemplate(_ context.Context, req AdoptLibraryTemplateRequest) (AdoptLibraryTemplateResult, error) {
	if s == nil {
		return AdoptLibraryTemplateResult{Class: CallSuccess}, nil
	}
	s.adopts = append(s.adopts, req)
	if s.adoptErr != nil {
		return AdoptLibraryTemplateResult{}, s.adoptErr
	}
	return s.adopt, nil
}

func (s *scriptedOpenAPI) ProbeApp(_ context.Context, req ProbeAppRequest) (ProbeAppResult, error) {
	if s == nil {
		return ProbeAppResult{Class: CallSuccess, Message: "连接成功"}, nil
	}
	s.appProbes = append(s.appProbes, req)
	if s.appProbeErr != nil {
		return ProbeAppResult{}, s.appProbeErr
	}
	result := s.appProbe
	if result.Class == CallSuccess && result.Message == "" {
		result.Message = "连接成功"
	}
	return result, nil
}

func (s *scriptedOpenAPI) MiniProgramLogin(_ context.Context, req MiniProgramLoginCall) (MiniProgramLoginResult, error) {
	if s == nil {
		return MiniProgramLoginResult{}, nil
	}
	s.miniLogins = append(s.miniLogins, req)
	if s.miniLoginErr != nil {
		return MiniProgramLoginResult{}, s.miniLoginErr
	}
	return s.miniLogin, nil
}

func (s *scriptedOpenAPI) ProbePlatform(_ context.Context, req ProbePlatformRequest) error {
	if s == nil {
		return nil
	}
	s.probes = append(s.probes, req)
	if s.acceptTicket != "" && req.VerifyTicket != s.acceptTicket {
		return errors.New("verify ticket was not accepted")
	}
	return s.probeErr
}

func (s *scriptedOpenAPI) AnswerFullNetwork(_ context.Context, req FullNetworkReply) error {
	if s == nil {
		return nil
	}
	s.replies = append(s.replies, req)
	return s.replyErr
}

func (s *scriptedOpenAPI) StartAuthorization(_ context.Context, req OpenAuthorizationStart) (*AuthorizationLink, error) {
	if s == nil {
		return &AuthorizationLink{URL: "https://mp.weixin.qq.com/cgi-bin/componentloginpage?pre_auth_code=scripted"}, nil
	}
	s.starts = append(s.starts, req)
	if s.authErr != nil {
		return nil, s.authErr
	}
	url := s.authLink
	if url == "" {
		url = "https://mp.weixin.qq.com/cgi-bin/componentloginpage?pre_auth_code=scripted"
	}
	return &AuthorizationLink{URL: url}, nil
}

func (s *scriptedOpenAPI) ExchangeWebAuthorization(_ context.Context, req ExchangeWebAuthorizationRequest) (WebAuthorizationExchange, error) {
	if s == nil {
		return WebAuthorizationExchange{}, errors.New("no web grant scripted")
	}
	s.webExchanges = append(s.webExchanges, req)
	if s.webGrantErr != nil {
		return WebAuthorizationExchange{}, s.webGrantErr
	}
	if s.webGrant == nil {
		return WebAuthorizationExchange{}, errors.New("no web grant scripted")
	}
	return *s.webGrant, nil
}

func (s *scriptedOpenAPI) CompleteAuthorization(_ context.Context, req OpenAuthorizationCode) (*AuthorizationGrant, error) {
	if s == nil {
		return nil, errors.New("no grant scripted")
	}
	s.completes = append(s.completes, req)
	if s.grantErr != nil {
		return nil, s.grantErr
	}
	if s.grant == nil {
		return nil, errors.New("no grant scripted")
	}
	cp := *s.grant
	if s.grant.Capabilities != nil {
		cp.Capabilities = append([]string(nil), s.grant.Capabilities...)
	}
	return &cp, nil
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
	return newScriptedPlatform(t, &scriptedOpenAPI{})
}

func newScriptedPlatform(t *testing.T, api *scriptedOpenAPI) Platform {
	t.Helper()
	svc, _ := newScriptedPlatformRepo(t, api)
	return svc
}

func newScriptedPlatformRepo(t *testing.T, api *scriptedOpenAPI) (Platform, *memPlatformRepo) {
	t.Helper()
	t.Setenv(crypt.EnvMasterKey, "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	repo := newMemPlatformRepo(crypt.New())
	return NewPlatformService(repo, api, NewPushAuthenticator(), testCallbackBase, nil, slog.New(slog.DiscardHandler)), repo
}

func mustGet(t *testing.T, svc Platform, id int64) *entity.Platform {
	t.Helper()
	got, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %d: %v", id, err)
	}
	return got
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
