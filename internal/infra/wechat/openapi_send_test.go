package wechatinfra

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
)

func TestSendPayloadCarriesMiniProgramAndPage(t *testing.T) {
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cgi-bin/token") {
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("body: %v", err)
		}
		bodies[r.URL.Path] = payload
		_, _ = w.Write([]byte(`{"errcode":0,"msgid":99}`))
	}))
	defer srv.Close()
	api := &openAPI{client: srv.Client(), base: srv.URL}
	req := wechatservice.SendMessageRequest{
		ProbeAppRequest:  wechatservice.ProbeAppRequest{Connection: entity.ConnectionDirectHold, AppID: "wx", AppSecret: "sec"},
		OpenID:           "openid-a",
		WechatTemplateID: "tmpl",
		Content:          "已发货",
		WebURL:           "https://shop.example/orders/1",
		MiniProgramAppID: "wxminiappid000001",
		MiniProgramPage:  "pages/order/index",
	}
	for _, channel := range []string{
		entity.CapabilityTemplateMessage,
		entity.CapabilitySubscribeNotification,
		entity.CapabilitySubscribeMessage,
	} {
		req.Channel = channel
		result, err := api.SendMessage(context.Background(), req)
		if err != nil || result.Class != wechatservice.CallSuccess {
			t.Fatalf("%s send = %+v %v", channel, result, err)
		}
	}
	template := bodies["/cgi-bin/message/template/send"]
	mini, _ := template["miniprogram"].(map[string]any)
	if template["url"] != req.WebURL || mini["appid"] != req.MiniProgramAppID || mini["pagepath"] != req.MiniProgramPage {
		t.Fatalf("template payload = %+v", template)
	}
	notice := bodies["/cgi-bin/message/subscribe/bizsend"]
	noticeMini, _ := notice["miniprogram"].(map[string]any)
	if notice["page"] != req.WebURL || noticeMini["appid"] != req.MiniProgramAppID || noticeMini["pagepath"] != req.MiniProgramPage {
		t.Fatalf("subscribe notification payload = %+v", notice)
	}
	message := bodies["/cgi-bin/message/subscribe/send"]
	if message["page"] != req.MiniProgramPage || message["url"] != req.WebURL {
		t.Fatalf("subscribe message payload = %+v", message)
	}
	if _, ok := message["miniprogram"]; ok {
		t.Fatalf("subscribe message used a miniprogram object: %+v", message)
	}
}

func TestAdoptSubscribeTemplateLoadsKeywordIDs(t *testing.T) {
	var added []int
	addCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/cgi-bin/token"):
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
		case strings.Contains(r.URL.Path, "/wxaapi/newtmpl/getpubtemplatekeywords"):
			_, _ = w.Write([]byte(`{"errcode":0,"data":[{"kid":3,"name":"物品"},{"kid":5,"name":"时间"},{"kid":9,"name":"备注"},{"kid":11,"name":"地点"},{"kid":12,"name":"金额"},{"kid":13,"name":"超出"}]}`))
		case strings.Contains(r.URL.Path, "/wxaapi/newtmpl/addtemplate"):
			addCalls++
			raw, _ := io.ReadAll(r.Body)
			var payload struct {
				KidList []int `json:"kidList"`
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Errorf("body: %v", err)
			}
			added = payload.KidList
			_, _ = w.Write([]byte(`{"errcode":0,"template_id":"wx-tmpl","title":"订单"}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	api := &openAPI{client: srv.Client(), base: srv.URL}
	result, err := api.AdoptLibraryTemplate(context.Background(), wechatservice.AdoptLibraryTemplateRequest{
		ProbeAppRequest:   wechatservice.ProbeAppRequest{Connection: entity.ConnectionDirectHold, AppID: "wx", AppSecret: "sec"},
		LibraryTemplateID: "TID00015",
		Channel:           entity.CapabilitySubscribeMessage,
	})
	if err != nil || result.Class != wechatservice.CallSuccess {
		t.Fatalf("adopt = %+v %v", result, err)
	}
	if addCalls != 1 || len(added) != addTemplateKeywordLimit || added[0] != 3 || added[4] != 12 {
		t.Fatalf("kidList = %v calls %d", added, addCalls)
	}
}

func TestAdoptSubscribeTemplateWithoutKeywordsDoesNotPost(t *testing.T) {
	addCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/cgi-bin/token"):
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
		case strings.Contains(r.URL.Path, "/wxaapi/newtmpl/getpubtemplatekeywords"):
			_, _ = w.Write([]byte(`{"errcode":0,"data":[]}`))
		case strings.Contains(r.URL.Path, "/wxaapi/newtmpl/addtemplate"):
			addCalls++
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	api := &openAPI{client: srv.Client(), base: srv.URL}
	result, err := api.AdoptLibraryTemplate(context.Background(), wechatservice.AdoptLibraryTemplateRequest{
		ProbeAppRequest:   wechatservice.ProbeAppRequest{Connection: entity.ConnectionDirectHold, AppID: "wx", AppSecret: "sec"},
		LibraryTemplateID: "TID00015",
		Channel:           entity.CapabilitySubscribeNotification,
	})
	if err != nil || result.Class != wechatservice.CallBusinessRejection {
		t.Fatalf("adopt = %+v %v", result, err)
	}
	if addCalls != 0 {
		t.Fatalf("empty keywords still posted addtemplate %d times", addCalls)
	}
}
