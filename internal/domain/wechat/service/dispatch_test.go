package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/listq"
)

type memTemplateRepo struct {
	rows   map[int64]*entity.AdoptedTemplate
	nextID int64
}

func newMemTemplateRepo() *memTemplateRepo {
	return &memTemplateRepo{rows: map[int64]*entity.AdoptedTemplate{}, nextID: 800}
}

func (r *memTemplateRepo) NextID() int64 { r.nextID++; return r.nextID }

func cloneTemplate(row *entity.AdoptedTemplate) *entity.AdoptedTemplate {
	if row == nil {
		return nil
	}
	cp := *row
	return &cp
}

func (r *memTemplateRepo) FindByID(_ context.Context, id int64) (*entity.AdoptedTemplate, error) {
	row, ok := r.rows[id]
	if !ok {
		return nil, wechat.ErrTemplateNotFound
	}
	return cloneTemplate(row), nil
}

func (r *memTemplateRepo) FindByAppAndLibrary(_ context.Context, appID int64, libraryTemplateID string) (*entity.AdoptedTemplate, error) {
	for _, row := range r.rows {
		if row.AppID == appID && row.LibraryTemplateID == libraryTemplateID {
			return cloneTemplate(row), nil
		}
	}
	return nil, wechat.ErrTemplateNotFound
}

func (r *memTemplateRepo) ListByApp(_ context.Context, appID int64) ([]*entity.AdoptedTemplate, error) {
	out := make([]*entity.AdoptedTemplate, 0)
	for _, row := range r.rows {
		if row.AppID == appID {
			out = append(out, cloneTemplate(row))
		}
	}
	return out, nil
}

type memDispatchRepo struct {
	rows   map[int64]*entity.Dispatch
	nextID int64
}

func newMemDispatchRepo() *memDispatchRepo {
	return &memDispatchRepo{rows: map[int64]*entity.Dispatch{}, nextID: 900}
}

func (r *memDispatchRepo) NextID() int64 { r.nextID++; return r.nextID }

func cloneDispatch(row *entity.Dispatch) *entity.Dispatch {
	if row == nil {
		return nil
	}
	cp := *row
	return &cp
}

func (r *memDispatchRepo) Create(_ context.Context, row *entity.Dispatch) (*entity.Dispatch, error) {
	stored := cloneDispatch(row)
	r.rows[stored.ID] = stored
	return cloneDispatch(stored), nil
}

func (r *memDispatchRepo) MarkDelivered(_ context.Context, req repository.MarkDeliveredRequest) error {
	msgid := strings.TrimSpace(req.MsgID)
	if msgid == "" {
		return nil
	}
	for _, row := range r.rows {
		if row.MsgID == msgid {
			row.Delivered = true
			row.UpdateBy = req.ActorID
		}
	}
	return nil
}

func (r *memDispatchRepo) List(_ context.Context, q repository.DispatchQuery) ([]*entity.Dispatch, error) {
	out := make([]*entity.Dispatch, 0)
	for _, row := range r.rows {
		if q.AppID != 0 && row.AppID != q.AppID {
			continue
		}
		out = append(out, cloneDispatch(row))
	}
	if q.Offset > 0 {
		if q.Offset >= len(out) {
			return []*entity.Dispatch{}, nil
		}
		out = out[q.Offset:]
	}
	if q.Limit > 0 && q.Limit < len(out) {
		out = out[:q.Limit]
	}
	return out, nil
}

func (r *memTemplateRepo) Create(_ context.Context, row *entity.AdoptedTemplate) (*entity.AdoptedTemplate, error) {
	stored := cloneTemplate(row)
	r.rows[stored.ID] = stored
	return cloneTemplate(stored), nil
}

func TestAdoptLibraryTemplateUsesTheLiveAppToken(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.adopt = AdoptLibraryTemplateResult{
		Class:            CallSuccess,
		WechatTemplateID: "wx-tmpl-9001",
		Title:            "订单发货通知",
	}

	adopted, err := f.apps.AdoptTemplate(context.Background(), AdoptTemplateRequest{
		AppID:             held.App.ID,
		LibraryTemplateID: "TM00015",
		Channel:           entity.CapabilityTemplateMessage,
		ActorID:           7,
	})
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if adopted == nil || adopted.AppID != held.App.ID || adopted.LibraryTemplateID != "TM00015" || adopted.WechatTemplateID != "wx-tmpl-9001" || adopted.Title != "订单发货通知" {
		t.Fatalf("adopted = %+v", adopted)
	}
	listed, err := f.apps.ListTemplates(context.Background(), held.App.ID)
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != adopted.ID || listed[0].LibraryTemplateID != "TM00015" {
		t.Fatalf("templates = %+v", listed)
	}
	if len(f.api.adopts) != 1 {
		t.Fatalf("adopts = %d, want the one library call", len(f.api.adopts))
	}
	call := f.api.adopts[0]
	if call.Connection != entity.ConnectionDirectHold || call.AppID != held.App.AppID || call.AppSecret != testAppSecret || call.LibraryTemplateID != "TM00015" || call.AuthorizerRefreshToken != "" {
		t.Fatalf("adopt call = %+v, want the app's current token and the library template", call)
	}
	if mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("adopting a template lapsed the app")
	}
}

func TestAdoptingTheSameLibraryTemplateAgainIsRefused(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.adopt = AdoptLibraryTemplateResult{Class: CallSuccess, WechatTemplateID: "wx-tmpl-9001", Title: "订单发货通知"}
	first, err := f.apps.AdoptTemplate(context.Background(), AdoptTemplateRequest{
		AppID:             held.App.ID,
		LibraryTemplateID: "TM00015",
		Channel:           entity.CapabilityTemplateMessage,
		ActorID:           7,
	})
	if err != nil {
		t.Fatalf("first adopt: %v", err)
	}
	f.api.adopt = AdoptLibraryTemplateResult{Class: CallSuccess, WechatTemplateID: "wx-tmpl-9002", Title: "另一条"}
	second, err := f.apps.AdoptTemplate(context.Background(), AdoptTemplateRequest{
		AppID:             held.App.ID,
		LibraryTemplateID: "TM00015",
		Channel:           entity.CapabilityTemplateMessage,
		ActorID:           8,
	})
	if !errors.Is(err, wechat.ErrTemplateDuplicate) || second != nil {
		t.Fatalf("second adopt = (%v, %v), want duplicate and no new template", second, err)
	}
	listed, err := f.apps.ListTemplates(context.Background(), held.App.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != first.ID || listed[0].WechatTemplateID != "wx-tmpl-9001" {
		t.Fatalf("templates = %+v", listed)
	}
	if len(f.api.adopts) != 1 {
		t.Fatalf("adopts = %d, want no second WeChat call", len(f.api.adopts))
	}
}

func TestUnusableAppRefusesTemplateAdoption(t *testing.T) {
	f := newAuthFixture(t)
	suspended, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("suspended hold: %v", err)
	}
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: suspended.App.ID, Suspended: true, ActorID: 8}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	lapsed, err := f.apps.Create(context.Background(), serviceHold("wxservice0000002", 7))
	if err != nil {
		t.Fatalf("lapsed hold: %v", err)
	}
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "密钥被拒绝"}
	if _, err := f.apps.Probe(context.Background(), lapsed.App.ID); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	withdrawn, err := f.apps.Create(context.Background(), serviceHold("wxservice0000003", 7))
	if err != nil {
		t.Fatalf("withdrawn hold: %v", err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: withdrawn.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantService(t, platform.ID, "wxservice0000099", []string{entity.CapabilityTemplateMessage})
	if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), platform.ID); err != nil {
		t.Fatalf("release ticket: %v", err)
	}
	f.api.adopt = AdoptLibraryTemplateResult{Class: CallSuccess, WechatTemplateID: "wx-tmpl-9001", Title: "订单发货通知"}
	f.api.adopts = nil

	cases := []struct {
		name  string
		appID int64
		want  error
	}{
		{name: "suspended", appID: suspended.App.ID, want: wechat.ErrAppSuspended},
		{name: "lapsed", appID: lapsed.App.ID, want: wechat.ErrAppLapsed},
		{name: "withdrawn", appID: withdrawn.App.ID, want: wechat.ErrAppNotFound},
		{name: "unready platform", appID: granted.ID, want: wechat.ErrPlatformNotReady},
	}
	for _, tc := range cases {
		adopted, err := f.apps.AdoptTemplate(context.Background(), AdoptTemplateRequest{
			AppID:             tc.appID,
			LibraryTemplateID: "TM00015",
			Channel:           entity.CapabilityTemplateMessage,
			ActorID:           7,
		})
		if !errors.Is(err, tc.want) || adopted != nil {
			t.Fatalf("%s adopt = (%v, %v), want %v", tc.name, adopted, err, tc.want)
		}
		listed, err := f.apps.ListTemplates(context.Background(), tc.appID)
		if err != nil {
			t.Fatalf("%s list: %v", tc.name, err)
		}
		if len(listed) != 0 {
			t.Fatalf("%s stored %+v", tc.name, listed)
		}
	}
	if len(f.api.adopts) != 0 {
		t.Fatalf("unusable apps called WeChat: %+v", f.api.adopts)
	}
	if !mustGetApp(t, f, lapsed.App.ID).Lapsed || mustGetApp(t, f, suspended.App.ID).Lapsed || mustGetApp(t, f, granted.ID).Lapsed {
		t.Fatal("refusing adoption changed lapse")
	}
}

func TestAcceptedSendRecordsTheDispatch(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}

	sent, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:      held.App.ID,
		TemplateID: template.ID,
		OpenID:     "openid-a",
		Content:    "已发货",
		WebURL:     "https://shop.example/orders/1",
		ActorID:    7,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent == nil || !sent.Accepted || sent.MsgID != "msgid-100" || sent.OpenID != "openid-a" || sent.TemplateID != template.ID || sent.Content != "已发货" || sent.WebURL != "https://shop.example/orders/1" || sent.MiniProgramPage != "" {
		t.Fatalf("dispatch = %+v", sent)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 1 || page.Dispatches[0].ID != sent.ID || page.Dispatches[0].MsgID != "msgid-100" {
		t.Fatalf("dispatches = %+v", page)
	}
	if len(f.api.sends) != 1 {
		t.Fatalf("sends = %d", len(f.api.sends))
	}
	call := f.api.sends[0]
	if call.AppID != held.App.AppID || call.AppSecret != testAppSecret || call.OpenID != "openid-a" || call.WechatTemplateID != "wx-tmpl-9001" || call.Content != "已发货" || call.WebURL != "https://shop.example/orders/1" || call.MiniProgramPage != "" || call.Channel != entity.CapabilityTemplateMessage {
		t.Fatalf("send call = %+v", call)
	}
	if mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("an accepted send lapsed the app")
	}
}

func TestSendWithoutTemplateOrOpenIDCreatesNoDispatch(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	other, err := f.apps.Create(context.Background(), serviceHold("wxservice0000002", 7))
	if err != nil {
		t.Fatalf("other hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	f.api.sends = nil

	missingOpenID, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:      held.App.ID,
		TemplateID: template.ID,
		OpenID:     "  ",
		Content:    "已发货",
		ActorID:    7,
	})
	if err == nil || missingOpenID != nil {
		t.Fatalf("blank openid = (%v, %v), want a refusal and no dispatch", missingOpenID, err)
	}
	foreign, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:      other.App.ID,
		TemplateID: template.ID,
		OpenID:     "openid-a",
		Content:    "已发货",
		ActorID:    7,
	})
	if !errors.Is(err, wechat.ErrTemplateNotFound) || foreign != nil {
		t.Fatalf("unadopted template = (%v, %v)", foreign, err)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 0 {
		t.Fatalf("dispatches = %+v, want none", page)
	}
	if len(f.api.sends) != 0 {
		t.Fatalf("invalid send called WeChat: %+v", f.api.sends)
	}
	if mustGetApp(t, f, held.App.ID).Lapsed || mustGetApp(t, f, other.App.ID).Lapsed {
		t.Fatal("a local refusal lapsed an app")
	}
}

func TestRejectedSendRecordsADispatchWithoutMsgID(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallBusinessRejection, Message: "用户未订阅"}
	sent, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:      held.App.ID,
		TemplateID: template.ID,
		OpenID:     "openid-a",
		Content:    "已发货",
		ActorID:    7,
	})
	if err == nil || sent == nil {
		t.Fatalf("rejected send = (%v, %v), want the refusal and a dispatch", sent, err)
	}
	if sent.Accepted || sent.MsgID != "" || sent.OpenID != "openid-a" || sent.TemplateID != template.ID || sent.Content != "已发货" {
		t.Fatalf("dispatch = %+v", sent)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 1 || page.Dispatches[0].Accepted || page.Dispatches[0].MsgID != "" {
		t.Fatalf("dispatches = %+v", page)
	}
	if mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("a business rejection lapsed the app")
	}
}

func TestSendTimeoutRecordsNoAcceptedDispatch(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallUnavailable, Message: "超时"}
	sent, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:      held.App.ID,
		TemplateID: template.ID,
		OpenID:     "openid-a",
		Content:    "已发货",
		ActorID:    7,
	})
	if err == nil || sent != nil {
		t.Fatalf("timeout = (%v, %v), want a failed call and no dispatch", sent, err)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 0 {
		t.Fatalf("timeout stored %+v", page)
	}
	if mustGetApp(t, f, held.App.ID).Lapsed {
		t.Fatal("a timeout lapsed the app")
	}
}

func TestSendDefinitiveRefusalLapsesWithoutAnAcceptedDispatch(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallDefinitiveRefusal, Message: "授权已被取消"}
	sent, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:      held.App.ID,
		TemplateID: template.ID,
		OpenID:     "openid-a",
		Content:    "已发货",
		ActorID:    7,
	})
	if err == nil || (sent != nil && sent.Accepted) {
		t.Fatalf("refusal = (%v, %v), want no accepted dispatch", sent, err)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range page.Dispatches {
		if row.Accepted {
			t.Fatalf("accepted dispatch after refusal: %+v", row)
		}
	}
	got := mustGetApp(t, f, held.App.ID)
	if !got.Lapsed || got.UpdateBy != 0 {
		t.Fatalf("after refusal = %+v, want lapsed by the system", got)
	}
}

func TestSecondSendIsANewDispatch(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	first, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID: held.App.ID, TemplateID: template.ID, OpenID: "openid-a", Content: "已发货", ActorID: 7,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	f.api.send = SendMessageResult{Class: CallUnavailable, Message: "超时"}
	if _, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID: held.App.ID, TemplateID: template.ID, OpenID: "openid-a", Content: "已发货", ActorID: 7,
	}); err == nil {
		t.Fatal("timeout did not fail the call")
	}
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-300"}
	second, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID: held.App.ID, TemplateID: template.ID, OpenID: "openid-a", Content: "再次通知", ActorID: 8,
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID == first.ID || second.MsgID != "msgid-300" || !second.Accepted {
		t.Fatalf("second = %+v, first id %d", second, first.ID)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{AppID: held.App.ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 2 {
		t.Fatalf("dispatches = %+v, want the accepted send and the later one, not the timeout", page)
	}
}

func TestUnusableAppRefusesSend(t *testing.T) {
	f := newAuthFixture(t)
	suspended, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("suspended: %v", err)
	}
	template := adoptTemplate(t, f, suspended.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: suspended.App.ID, Suspended: true, ActorID: 8}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	lapsed, err := f.apps.Create(context.Background(), serviceHold("wxservice0000002", 7))
	if err != nil {
		t.Fatalf("lapsed: %v", err)
	}
	lapsedTemplate := adoptTemplate(t, f, lapsed.App.ID, "TM00016", entity.CapabilityTemplateMessage, "wx-tmpl-9002")
	f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "密钥被拒绝"}
	if _, err := f.apps.Probe(context.Background(), lapsed.App.ID); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	withdrawn, err := f.apps.Create(context.Background(), serviceHold("wxservice0000003", 7))
	if err != nil {
		t.Fatalf("withdrawn: %v", err)
	}
	withdrawnTemplate := adoptTemplate(t, f, withdrawn.App.ID, "TM00017", entity.CapabilityTemplateMessage, "wx-tmpl-9003")
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: withdrawn.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantService(t, platform.ID, "wxservice0000099", []string{entity.CapabilityTemplateMessage})
	grantedTemplate := adoptTemplate(t, f, granted.ID, "TM00018", entity.CapabilityTemplateMessage, "wx-tmpl-9004")
	if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), platform.ID); err != nil {
		t.Fatalf("release ticket: %v", err)
	}
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	f.api.sends = nil

	cases := []struct {
		name  string
		appID int64
		tpl   int64
		want  error
	}{
		{"suspended", suspended.App.ID, template.ID, wechat.ErrAppSuspended},
		{"lapsed", lapsed.App.ID, lapsedTemplate.ID, wechat.ErrAppLapsed},
		{"withdrawn", withdrawn.App.ID, withdrawnTemplate.ID, wechat.ErrAppNotFound},
		{"unready platform", granted.ID, grantedTemplate.ID, wechat.ErrPlatformNotReady},
	}
	for _, tc := range cases {
		sent, err := f.apps.Send(context.Background(), sendOf(tc.appID, tc.tpl))
		if !errors.Is(err, tc.want) || sent != nil {
			t.Fatalf("%s send = (%v, %v), want %v", tc.name, sent, err, tc.want)
		}
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 0 || len(f.api.sends) != 0 {
		t.Fatalf("unusable send stored %+v calls %+v", page, f.api.sends)
	}
	if mustGetApp(t, f, suspended.App.ID).Lapsed || !mustGetApp(t, f, lapsed.App.ID).Lapsed || mustGetApp(t, f, granted.ID).Lapsed {
		t.Fatal("refusing a send changed lapse")
	}
}

func TestDispatchListKeepsWithdrawnAppsAndFiltersByApp(t *testing.T) {
	f := newAuthFixture(t)
	live, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	gone, err := f.apps.Create(context.Background(), serviceHold("wxservice0000002", 7))
	if err != nil {
		t.Fatalf("gone: %v", err)
	}
	liveTemplate := adoptTemplate(t, f, live.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	goneTemplate := adoptTemplate(t, f, gone.App.ID, "TM00016", entity.CapabilityTemplateMessage, "wx-tmpl-9002")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-live"}
	liveSend, err := f.apps.Send(context.Background(), sendOf(live.App.ID, liveTemplate.ID))
	if err != nil {
		t.Fatalf("live send: %v", err)
	}
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-gone"}
	goneSend, err := f.apps.Send(context.Background(), sendOf(gone.App.ID, goneTemplate.ID))
	if err != nil {
		t.Fatalf("gone send: %v", err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: gone.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	all, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if !dispatchIDs(all, liveSend.ID, goneSend.ID) {
		t.Fatalf("all = %+v, want both attempts", all)
	}
	filtered, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{AppID: gone.App.ID})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if filtered == nil || len(filtered.Dispatches) != 1 || filtered.Dispatches[0].ID != goneSend.ID || filtered.Dispatches[0].MsgID != "msgid-gone" {
		t.Fatalf("filtered = %+v", filtered)
	}
	templates, err := f.apps.ListTemplates(context.Background(), gone.App.ID)
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	if len(templates) != 1 || templates[0].ID != goneTemplate.ID {
		t.Fatalf("withdrawing the app withdrew templates: %+v", templates)
	}
	apps, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("apps: %v", err)
	}
	if findApp(apps, gone.App.ID) != nil {
		t.Fatal("ordinary app list includes a withdrawn app")
	}
	if findApp(apps, live.App.ID) == nil {
		t.Fatal("ordinary app list dropped the live app")
	}
}

func dispatchIDs(page *DispatchPage, ids ...int64) bool {
	if page == nil || len(page.Dispatches) != len(ids) {
		return false
	}
	seen := map[int64]bool{}
	for _, row := range page.Dispatches {
		seen[row.ID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			return false
		}
	}
	return true
}

func TestKindAndCapabilityRefusedLocally(t *testing.T) {
	f := newAuthFixture(t)
	serviceApp, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	subscription, err := f.apps.Create(context.Background(), subscriptionHold("wxsubscrib000001", 7))
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	mini, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("mini: %v", err)
	}
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	partial := f.grantService(t, platform.ID, "wxservice0000099", []string{entity.CapabilityTemplateMessage})
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}

	serviceTemplate := adoptTemplate(t, f, serviceApp.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	serviceNotice := adoptTemplate(t, f, serviceApp.App.ID, "TID00015", entity.CapabilitySubscribeNotification, "wx-tmpl-9002")
	subTemplate := adoptTemplate(t, f, subscription.App.ID, "TM00016", entity.CapabilityTemplateMessage, "wx-tmpl-9003")
	subNotice := adoptTemplate(t, f, subscription.App.ID, "TID00016", entity.CapabilitySubscribeNotification, "wx-tmpl-9004")
	miniMessage := adoptTemplate(t, f, mini.App.ID, "TID00017", entity.CapabilitySubscribeMessage, "wx-tmpl-9005")
	miniTemplate := adoptTemplate(t, f, mini.App.ID, "TM00017", entity.CapabilityTemplateMessage, "wx-tmpl-9006")
	miniNotice := adoptTemplate(t, f, mini.App.ID, "TID00018", entity.CapabilitySubscribeNotification, "wx-tmpl-9007")
	officialMini := adoptTemplate(t, f, serviceApp.App.ID, "TID00019", entity.CapabilitySubscribeMessage, "wx-tmpl-9008")
	granted := adoptTemplate(t, f, partial.ID, "TM00018", entity.CapabilityTemplateMessage, "wx-tmpl-9009")
	missing := adoptTemplate(t, f, partial.ID, "TID00020", entity.CapabilitySubscribeNotification, "wx-tmpl-9010")
	f.api.sends = nil

	if _, err := f.apps.Send(context.Background(), sendOf(serviceApp.App.ID, serviceNotice.ID)); err != nil {
		t.Fatalf("service account subscribe notification: %v", err)
	}
	if _, err := f.apps.Send(context.Background(), sendOf(mini.App.ID, miniMessage.ID)); err != nil {
		t.Fatalf("mini program subscribe message: %v", err)
	}
	if _, err := f.apps.Send(context.Background(), sendOf(partial.ID, granted.ID)); err != nil {
		t.Fatalf("granted template message: %v", err)
	}
	refused := []struct {
		name string
		app  int64
		tpl  int64
	}{
		{"subscription template message", subscription.App.ID, subTemplate.ID},
		{"subscription subscribe notification", subscription.App.ID, subNotice.ID},
		{"mini program template message", mini.App.ID, miniTemplate.ID},
		{"mini program subscribe notification", mini.App.ID, miniNotice.ID},
		{"service account subscribe message", serviceApp.App.ID, officialMini.ID},
		{"platform missing subscribe notification", partial.ID, missing.ID},
	}
	calls := len(f.api.sends)
	for _, tc := range refused {
		sent, err := f.apps.Send(context.Background(), sendOf(tc.app, tc.tpl))
		if !errors.Is(err, wechat.ErrCapabilityDenied) || sent != nil {
			t.Fatalf("%s = (%v, %v)", tc.name, sent, err)
		}
	}
	if len(f.api.sends) != calls {
		t.Fatalf("a local refusal called WeChat: %d sends, want %d", len(f.api.sends), calls)
	}
	for _, id := range []int64{serviceApp.App.ID, subscription.App.ID, mini.App.ID, partial.ID} {
		if mustGetApp(t, f, id).Lapsed {
			t.Fatalf("app %d lapsed on a local refusal", id)
		}
	}
	_ = serviceTemplate
}

func sendOf(appID, templateID int64) SendDispatchRequest {
	return SendDispatchRequest{
		AppID:      appID,
		TemplateID: templateID,
		OpenID:     "openid-a",
		Content:    "已发货",
		ActorID:    7,
	}
}

func subscriptionHold(appID string, actorID int64) CreateDirectHoldRequest {
	return CreateDirectHoldRequest{
		Name:      "演示订阅号",
		AppID:     appID,
		AppType:   entity.AppTypeOfficialAccount,
		Kind:      entity.AccountKindSubscription,
		AppSecret: testAppSecret,
		ActorID:   actorID,
	}
}

func TestSendRecordsWebURLAndMiniProgramPage(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, held.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-200"}
	sent, err := f.apps.Send(context.Background(), SendDispatchRequest{
		AppID:           held.App.ID,
		TemplateID:      template.ID,
		OpenID:          "openid-a",
		Content:         "已发货",
		WebURL:          "https://shop.example/orders/1",
		MiniProgramPage: "pages/order/index?id=1",
		ActorID:         7,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.WebURL != "https://shop.example/orders/1" || sent.MiniProgramPage != "pages/order/index?id=1" || sent.MsgID != "msgid-200" || !sent.Accepted {
		t.Fatalf("dispatch = %+v", sent)
	}
	if len(f.api.sends) != 1 || f.api.sends[0].WebURL != sent.WebURL || f.api.sends[0].MiniProgramPage != sent.MiniProgramPage {
		t.Fatalf("send call = %+v", f.api.sends)
	}
}

func adoptTemplate(t *testing.T, f *authFixture, appID int64, libraryID, channel, wechatID string) *entity.AdoptedTemplate {
	t.Helper()
	f.api.adopt = AdoptLibraryTemplateResult{Class: CallSuccess, WechatTemplateID: wechatID, Title: "订单发货通知"}
	adopted, err := f.apps.AdoptTemplate(context.Background(), AdoptTemplateRequest{
		AppID:             appID,
		LibraryTemplateID: libraryID,
		Channel:           channel,
		ActorID:           7,
	})
	if err != nil {
		t.Fatalf("adopt %s: %v", libraryID, err)
	}
	return adopted
}

func TestAdoptDefinitiveRefusalLapsesAndStoresNoTemplate(t *testing.T) {
	f := newAuthFixture(t)
	held, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	f.api.adopt = AdoptLibraryTemplateResult{Class: CallDefinitiveRefusal, Message: "授权已被取消"}
	adopted, err := f.apps.AdoptTemplate(context.Background(), AdoptTemplateRequest{
		AppID:             held.App.ID,
		LibraryTemplateID: "TM00015",
		Channel:           entity.CapabilityTemplateMessage,
		ActorID:           7,
	})
	if err == nil || adopted != nil {
		t.Fatalf("adopt = (%v, %v), want a refusal and no template", adopted, err)
	}
	listed, err := f.apps.ListTemplates(context.Background(), held.App.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("refusal stored %+v", listed)
	}
	got := mustGetApp(t, f, held.App.ID)
	if !got.Lapsed || got.UpdateBy != 0 {
		t.Fatalf("after refusal = %+v, want lapsed by the system", got)
	}
}

func (f *authFixture) grantService(t *testing.T, platformID int64, appID string, caps []string) *entity.App {
	t.Helper()
	f.script(AuthorizationGrant{
		AppID:        appID,
		Name:         "授权服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
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
