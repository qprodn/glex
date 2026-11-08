package service

import (
	"context"
	"errors"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/pkg/listq"
)

// deliveryReportXML is a WeChat template-message delivery report. The
// msgid is the one the earlier send stored.
func deliveryReportXML(msgid string) string {
	return "<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1395658920</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[TEMPLATESENDJOBFINISH]]></Event><MsgID>" + msgid + "</MsgID><Status><![CDATA[success]]></Status></xml>"
}

func TestDeliveryReportUpdatesTheSameDispatch(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantService(t, platform.ID, "wxservice0000001", []string{entity.CapabilityTemplateMessage})
	template := adoptTemplate(t, f, granted.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	sent, err := f.apps.Send(context.Background(), sendOf(granted.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.Delivered {
		t.Fatal("acceptance is already delivery")
	}

	outcome, err := f.platforms.Receive(context.Background(), encryptedPush(t, platform.ID, platform.ComponentAppID, deliveryReportXML("msgid-100")))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if outcome == nil || outcome.Body != "success" {
		t.Fatalf("outcome = %+v, want an acknowledgement", outcome)
	}
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 1 {
		t.Fatalf("dispatches = %+v, want the one attempt", page)
	}
	got := page.Dispatches[0]
	if got.ID != sent.ID || got.MsgID != "msgid-100" || !got.Accepted || !got.Delivered || got.UpdateBy != 0 || got.CreateBy != 7 || got.OpenID != "openid-a" || got.Content != "已发货" {
		t.Fatalf("dispatch = %+v, want the same attempt marked delivered by the system", got)
	}
	if mustGetApp(t, f, granted.ID).Lapsed {
		t.Fatal("a delivery report lapsed the app")
	}
}

func TestDirectHoldDeliveryReportUpdatesTheSameDispatch(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, reveal.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	sent, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	outcome, err := f.apps.Receive(context.Background(), sealedAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID, deliveryReportXML("msgid-100")))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if outcome == nil || outcome.Body != "success" {
		t.Fatalf("outcome = %+v, want an acknowledgement and no automatic reply", outcome)
	}
	got := mustOneDispatch(t, f)
	if got.ID != sent.ID || got.MsgID != "msgid-100" || !got.Accepted || !got.Delivered || got.UpdateBy != 0 || got.CreateBy != 7 {
		t.Fatalf("dispatch = %+v, want the same attempt marked delivered by the system", got)
	}
	if mustGetApp(t, f, reveal.App.ID).Lapsed {
		t.Fatal("a delivery report lapsed the app")
	}
}

func TestDeliveryReportUpdatesADispatchAfterTheAppIsWithdrawn(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	heldTemplate := adoptTemplate(t, f, reveal.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-hold"}
	heldSend, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, heldTemplate.ID))
	if err != nil {
		t.Fatalf("direct send: %v", err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: reveal.App.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw direct hold: %v", err)
	}

	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	granted := f.grantService(t, platform.ID, "wxservice0000002", []string{entity.CapabilityTemplateMessage})
	grantedTemplate := adoptTemplate(t, f, granted.ID, "TM00016", entity.CapabilityTemplateMessage, "wx-tmpl-9002")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-grant"}
	grantedSend, err := f.apps.Send(context.Background(), sendOf(granted.ID, grantedTemplate.ID))
	if err != nil {
		t.Fatalf("platform send: %v", err)
	}
	if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: granted.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw granted app: %v", err)
	}

	heldOutcome, err := f.apps.Receive(context.Background(), sealedAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID, deliveryReportXML("msgid-hold")))
	if err != nil {
		t.Fatalf("direct callback after withdrawal: %v", err)
	}
	grantOutcome, err := f.platforms.Receive(context.Background(), encryptedPush(t, platform.ID, platform.ComponentAppID, deliveryReportXML("msgid-grant")))
	if err != nil {
		t.Fatalf("platform callback after withdrawal: %v", err)
	}
	if heldOutcome.Body != "success" || grantOutcome.Body != "success" {
		t.Fatalf("outcomes = %q %q, want both acknowledged", heldOutcome.Body, grantOutcome.Body)
	}

	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 2 {
		t.Fatalf("dispatches = %+v, want the two attempts and no new row", page)
	}
	seen := map[int64]*entity.Dispatch{}
	for _, row := range page.Dispatches {
		seen[row.ID] = row
	}
	held := seen[heldSend.ID]
	grantedRow := seen[grantedSend.ID]
	if held == nil || !held.Delivered || held.MsgID != "msgid-hold" || held.UpdateBy != 0 || !held.Accepted {
		t.Fatalf("withdrawn direct hold dispatch = %+v", held)
	}
	if grantedRow == nil || !grantedRow.Delivered || grantedRow.MsgID != "msgid-grant" || grantedRow.UpdateBy != 0 || !grantedRow.Accepted {
		t.Fatalf("withdrawn platform dispatch = %+v", grantedRow)
	}
	for _, id := range []int64{reveal.App.ID, granted.ID} {
		got, err := f.apps.Get(context.Background(), id)
		if !errors.Is(err, wechat.ErrAppNotFound) || got != nil {
			t.Fatalf("withdrawn app %d = (%v, %v), want it to stay withdrawn", id, got, err)
		}
	}
}

func TestUnknownDeliveryReportCreatesNoDispatch(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, reveal.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	sent, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")

	heldOutcome, err := f.apps.Receive(context.Background(), sealedAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID, deliveryReportXML("msgid-missing")))
	if err != nil {
		t.Fatalf("direct callback: %v", err)
	}
	grantOutcome, err := f.platforms.Receive(context.Background(), encryptedPush(t, platform.ID, platform.ComponentAppID, deliveryReportXML("msgid-no-such")))
	if err != nil {
		t.Fatalf("platform callback: %v", err)
	}
	if heldOutcome.Body != "success" || grantOutcome.Body != "success" {
		t.Fatalf("outcomes = %q %q, want both acknowledged", heldOutcome.Body, grantOutcome.Body)
	}
	got := mustOneDispatch(t, f)
	if got.ID != sent.ID || got.Delivered || got.MsgID != "msgid-100" || got.UpdateBy != 7 || !got.Accepted {
		t.Fatalf("dispatch = %+v, want the original attempt unchanged", got)
	}
	if mustGetApp(t, f, reveal.App.ID).Lapsed {
		t.Fatal("an unknown delivery lapsed the app")
	}
	listed, err := f.platforms.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("platforms: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("platforms = %d, want no app created from the unknown report", len(listed))
	}
}

func TestVerifiedNonDeliveryPushesAreAcknowledgedAndStoredNowhere(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, reveal.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	sent, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.api.sends = nil
	f.api.replies = nil

	events := []string{
		"<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1395658920</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[subscribe]]></Event></xml>",
		"<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1395658920</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[hello]]></Content><MsgId>msgid-100</MsgId></xml>",
		"<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1395658920</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[CLICK]]></Event><EventKey><![CDATA[menu-1]]></EventKey></xml>",
	}
	for _, plain := range events {
		held, err := f.apps.Receive(context.Background(), sealedAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID, plain))
		if err != nil {
			t.Fatalf("direct callback: %v", err)
		}
		pushed, err := f.platforms.Receive(context.Background(), encryptedPush(t, platform.ID, platform.ComponentAppID, plain))
		if err != nil {
			t.Fatalf("platform callback: %v", err)
		}
		if held.Body != "success" || pushed.Body != "success" {
			t.Fatalf("outcomes = %q %q, want an acknowledgement and no automatic reply", held.Body, pushed.Body)
		}
	}
	got := mustOneDispatch(t, f)
	if got.ID != sent.ID || got.Delivered || got.MsgID != "msgid-100" || got.UpdateBy != 7 || !got.Accepted {
		t.Fatalf("dispatch = %+v, want the send left untouched", got)
	}
	if len(f.api.sends) != 0 || len(f.api.replies) != 0 {
		t.Fatalf("non-delivery push called WeChat: sends %d replies %d", len(f.api.sends), len(f.api.replies))
	}
	if mustGetApp(t, f, reveal.App.ID).Lapsed {
		t.Fatal("a verified non-delivery push lapsed the app")
	}
	apps, err := f.apps.List(context.Background(), listq.Query{})
	if err != nil {
		t.Fatalf("apps: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != reveal.App.ID {
		t.Fatalf("apps = %+v, want no stored event and no new app", apps)
	}
}

func TestPlaintextCompatibleAndBadSignatureChangeNoAppAndNoDispatch(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, reveal.App.ID, "TM00015", entity.CapabilityTemplateMessage, "wx-tmpl-9001")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "msgid-100"}
	sent, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	beforeApp := mustGetApp(t, f, reveal.App.ID)
	beforePlatform := mustGet(t, f.platforms, platform.ID)

	rejects := []AppPushRequest{
		plaintextAppPush(reveal.App.ID, reveal.Token),
		compatibleAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID),
		badSignatureAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID),
	}
	for _, req := range rejects {
		if _, err := f.apps.Receive(context.Background(), req); !errors.Is(err, wechat.ErrPushRejected) {
			t.Fatalf("direct callback err = %v, want a rejection", err)
		}
	}
	platformRejects := []PushRequest{
		plaintextPush(platform.ID),
		compatiblePush(t, platform.ID, platform.ComponentAppID),
		badSignaturePush(t, platform.ID, platform.ComponentAppID),
	}
	for _, req := range platformRejects {
		if _, err := f.platforms.Receive(context.Background(), req); !errors.Is(err, wechat.ErrPushRejected) {
			t.Fatalf("platform callback err = %v, want a rejection", err)
		}
	}

	got := mustOneDispatch(t, f)
	if got.ID != sent.ID || got.Delivered || got.MsgID != "msgid-100" || got.UpdateBy != 7 || !got.Accepted || got.Content != "已发货" {
		t.Fatalf("dispatch = %+v, want it unchanged", got)
	}
	afterApp := mustGetApp(t, f, reveal.App.ID)
	if afterApp.Lapsed || afterApp.Suspended || afterApp.UpdateBy != beforeApp.UpdateBy || afterApp.Name != beforeApp.Name || afterApp.AppID != beforeApp.AppID {
		t.Fatalf("app changed from %+v to %+v", beforeApp, afterApp)
	}
	afterPlatform := mustGet(t, f.platforms, platform.ID)
	if !afterPlatform.Ready || afterPlatform.UpdateBy != beforePlatform.UpdateBy || operatorView(afterPlatform) != operatorView(beforePlatform) {
		t.Fatalf("platform changed from %s to %s", operatorView(beforePlatform), operatorView(afterPlatform))
	}
}

func TestSubscribeDeliveryReportUpdatesTheSameDispatch(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxservice0000001", 7))
	if err != nil {
		t.Fatalf("direct hold: %v", err)
	}
	template := adoptTemplate(t, f, reveal.App.ID, "TID00015", entity.CapabilitySubscribeNotification, "wx-tmpl-9002")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "1700827132819554304"}
	sent, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	plain := "<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1610969440</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[subscribe_msg_sent_event]]></Event><SubscribeMsgSentEvent><List><TemplateId><![CDATA[wx-tmpl-9002]]></TemplateId><MsgID>1700827132819554304</MsgID><ErrorCode>0</ErrorCode><ErrorStatus><![CDATA[success]]></ErrorStatus></List></SubscribeMsgSentEvent></xml>"
	outcome, err := f.apps.Receive(context.Background(), sealedAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID, plain))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if outcome == nil || outcome.Body != "success" {
		t.Fatalf("outcome = %+v, want an acknowledgement", outcome)
	}
	got := mustOneDispatch(t, f)
	if got.ID != sent.ID || !got.Delivered || got.MsgID != "1700827132819554304" || got.UpdateBy != 0 || !got.Accepted {
		t.Fatalf("dispatch = %+v, want the subscribe delivery written onto the same attempt", got)
	}
}

func TestMiniProgramDeliveryReportUpdatesTheSameDispatch(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), miniHold("wxminiappid000001", 7))
	if err != nil {
		t.Fatalf("mini program: %v", err)
	}
	template := adoptTemplate(t, f, reveal.App.ID, "TID00017", entity.CapabilitySubscribeMessage, "wx-tmpl-9005")
	f.api.send = SendMessageResult{Class: CallSuccess, MsgID: "1700827132819554304"}
	sent, err := f.apps.Send(context.Background(), sendOf(reveal.App.ID, template.ID))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	plain := "<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1610969440</CreateTime><MsgType><![CDATA[event]]></MsgType><Event><![CDATA[subscribe_msg_sent_event]]></Event><List><TemplateId><![CDATA[wx-tmpl-9005]]></TemplateId><MsgID>1700827132819554304</MsgID><ErrorCode>0</ErrorCode><ErrorStatus><![CDATA[success]]></ErrorStatus></List></xml>"
	outcome, err := f.apps.Receive(context.Background(), sealedAppPush(t, reveal.App.ID, reveal.Token, reveal.EncodingAESKey, reveal.App.AppID, plain))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if outcome == nil || outcome.Body != "success" {
		t.Fatalf("outcome = %+v, want an acknowledgement", outcome)
	}
	got := mustOneDispatch(t, f)
	if got.ID != sent.ID || !got.Delivered || got.MsgID != "1700827132819554304" || got.UpdateBy != 0 || !got.Accepted {
		t.Fatalf("dispatch = %+v, want the mini program delivery written onto the same attempt", got)
	}
}

func plaintextAppPush(id int64, token string) AppPushRequest {
	const timestamp = "1715943329"
	const nonce = "1590219412"
	return AppPushRequest{
		ID:          id,
		Timestamp:   timestamp,
		Nonce:       nonce,
		EncryptType: "",
		Signature:   msgSignature(token, timestamp, nonce, ""),
		Body:        []byte("<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><CreateTime>1395658920</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[hello]]></Content></xml>"),
	}
}

func compatibleAppPush(t *testing.T, id int64, token, aesKey, appID string) AppPushRequest {
	t.Helper()
	sealed := sealedAppPush(t, id, token, aesKey, appID, "<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[hello]]></Content></xml>")
	enc := encryptField(t, sealed.Body)
	sealed.Body = []byte("<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><FromUserName><![CDATA[openid-a]]></FromUserName><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[hello]]></Content><Encrypt><![CDATA[" + enc + "]]></Encrypt></xml>")
	return sealed
}

func badSignatureAppPush(t *testing.T, id int64, token, aesKey, appID string) AppPushRequest {
	t.Helper()
	req := sealedAppPush(t, id, token, aesKey, appID, deliveryReportXML("msgid-100"))
	req.MsgSignature = "0000000000000000000000000000000000000000"
	return req
}

func sealedAppPush(t *testing.T, id int64, token, aesKey, appID, plain string) AppPushRequest {
	t.Helper()
	const timestamp = "1715943329"
	const nonce = "1590219412"
	enc, err := (wxCrypto{}).encrypt(aesKey, appID, []byte(plain))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return AppPushRequest{
		ID:           id,
		Timestamp:    timestamp,
		Nonce:        nonce,
		EncryptType:  "aes",
		MsgSignature: msgSignature(token, timestamp, nonce, enc),
		Body:         []byte("<xml><ToUserName><![CDATA[gh_receiver]]></ToUserName><Encrypt><![CDATA[" + enc + "]]></Encrypt></xml>"),
	}
}

func mustOneDispatch(t *testing.T, f *authFixture) *entity.Dispatch {
	t.Helper()
	page, err := f.apps.ListDispatches(context.Background(), ListDispatchesRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page == nil || len(page.Dispatches) != 1 {
		t.Fatalf("dispatches = %+v, want one attempt", page)
	}
	return page.Dispatches[0]
}
