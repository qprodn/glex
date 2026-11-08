package service

import (
	"context"
	"errors"
	"testing"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
)

func TestServiceAccountSavesCustomMenuWithoutCallingWeChat(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	menu := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name:   "官网",
		WebURL: "https://example.com/home",
	}}}
	saved, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{
		ID:      reveal.App.ID,
		Menu:    menu,
		ActorID: 11,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved == nil || saved.Lapsed {
		t.Fatalf("saved = %+v", saved)
	}
	if !sameMenu(saved.SavedMenu, &menu) {
		t.Fatalf("saved menu = %+v", saved.SavedMenu)
	}
	if saved.AcceptedMenu != nil {
		t.Fatalf("accepted menu = %+v, want none", saved.AcceptedMenu)
	}
	if saved.UpdateBy != 11 || saved.CreateBy != 7 {
		t.Fatalf("audit = create_by %d update_by %d", saved.CreateBy, saved.UpdateBy)
	}
	if len(f.api.menuPublishes) != 0 || len(f.api.appProbes) != 0 {
		t.Fatal("saving a menu called WeChat")
	}
	got, err := f.apps.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !sameMenu(got.SavedMenu, &menu) || got.AcceptedMenu != nil || got.Lapsed || got.UpdateBy != 11 {
		t.Fatalf("reread saved=%+v accepted=%+v lapsed=%v update_by=%d", got.SavedMenu, got.AcceptedMenu, got.Lapsed, got.UpdateBy)
	}
}

func TestMenuLeafOpensPageOrMiniProgramAndParentOnlyHoldsChildren(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	valid := entity.CustomMenu{Buttons: []entity.MenuButton{
		{Name: "官网", WebURL: "https://example.com/home"},
		{Name: "商城", MiniProgramAppID: "wxminiappid000001", MiniProgramPage: "pages/index"},
		{Name: "更多", SubButtons: []entity.MenuButton{
			{Name: "活动", WebURL: "https://example.com/event"},
		}},
	}}
	if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: valid, ActorID: 11}); err != nil {
		t.Fatalf("save valid: %v", err)
	}
	invalid := []entity.CustomMenu{
		{Buttons: []entity.MenuButton{{
			Name: "父级还打开网页", WebURL: "https://example.com/parent",
			SubButtons: []entity.MenuButton{{Name: "子按钮", WebURL: "https://example.com/child"}},
		}}},
		{Buttons: []entity.MenuButton{{
			Name: "父级还跳小程序", MiniProgramAppID: "wxminiappid000001", MiniProgramPage: "pages/index",
			SubButtons: []entity.MenuButton{{Name: "子按钮", WebURL: "https://example.com/child"}},
		}}},
		{Buttons: []entity.MenuButton{{
			Name: "网页和小程序", WebURL: "https://example.com/both",
			MiniProgramAppID: "wxminiappid000001", MiniProgramPage: "pages/index",
		}}},
		{Buttons: []entity.MenuButton{{Name: "没有动作"}}},
		{Buttons: []entity.MenuButton{{Name: "只有小程序", MiniProgramAppID: "wxminiappid000001"}}},
		{Buttons: []entity.MenuButton{{WebURL: "https://example.com/noname"}}},
		{},
	}
	for _, menu := range invalid {
		saved, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menu, ActorID: 12})
		if !errors.Is(err, wechat.ErrMenuInvalid) || saved != nil {
			t.Fatalf("invalid menu %+v saved=(%v) err=%v", menu, saved, err)
		}
	}
	if len(f.api.menuPublishes) != 0 || len(f.api.appProbes) != 0 {
		t.Fatal("a menu shape check called WeChat")
	}
	got, err := f.apps.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !sameMenu(got.SavedMenu, &valid) || got.AcceptedMenu != nil || got.Lapsed || got.UpdateBy != 11 {
		t.Fatalf("invalid save changed the menu: saved=%+v accepted=%+v lapsed=%v update_by=%d", got.SavedMenu, got.AcceptedMenu, got.Lapsed, got.UpdateBy)
	}
}

func TestSavedMenuKeepsSubButtonsPastThreeTopLevelButtons(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	menu := entity.CustomMenu{Buttons: []entity.MenuButton{
		{Name: "一", WebURL: "https://example.com/1"},
		{Name: "二", WebURL: "https://example.com/2"},
		{Name: "三", WebURL: "https://example.com/3"},
		{Name: "四", SubButtons: []entity.MenuButton{
			{Name: "四一", WebURL: "https://example.com/4/1"},
			{Name: "四二", SubButtons: []entity.MenuButton{
				{Name: "四二一", MiniProgramAppID: "wxminiappid000009", MiniProgramPage: "pages/deep"},
			}},
		}},
	}}
	saved, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menu, ActorID: 11})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !sameMenu(saved.SavedMenu, &menu) || saved.AcceptedMenu != nil || saved.Lapsed {
		t.Fatalf("saved = %+v accepted %+v lapsed %v", saved.SavedMenu, saved.AcceptedMenu, saved.Lapsed)
	}
	if len(f.api.menuPublishes) != 0 {
		t.Fatal("saving a nested menu called WeChat")
	}
}

func TestPublishSendsTheWholeSavedMenuAsTheAcceptedDefault(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	first := entity.CustomMenu{Buttons: []entity.MenuButton{
		{Name: "官网", WebURL: "https://example.com/home"},
		{Name: "更多", SubButtons: []entity.MenuButton{
			{Name: "活动", WebURL: "https://example.com/event"},
			{Name: "商城", MiniProgramAppID: "wxminiappid000001", MiniProgramPage: "pages/mall"},
		}},
	}}
	if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: first, ActorID: 11}); err != nil {
		t.Fatalf("save first: %v", err)
	}
	f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
	published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 11})
	if err != nil || published == nil || published.Lapsed {
		t.Fatalf("publish = (%v, %v)", published, err)
	}
	if !sameMenu(published.SavedMenu, &first) || !sameMenu(published.AcceptedMenu, &first) {
		t.Fatalf("published saved=%+v accepted=%+v", published.SavedMenu, published.AcceptedMenu)
	}
	if len(f.api.menuPublishes) != 1 || !sameMenu(&f.api.menuPublishes[0].Menu, &first) || f.api.menuPublishes[0].AppID != "wxsvc000000000001" {
		t.Fatalf("publish call = %+v", f.api.menuPublishes)
	}

	second := entity.CustomMenu{Buttons: []entity.MenuButton{
		{Name: "新官网", WebURL: "https://example.com/new"},
	}}
	draft, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: second, ActorID: 12})
	if err != nil {
		t.Fatalf("save second: %v", err)
	}
	if !sameMenu(draft.SavedMenu, &second) || !sameMenu(draft.AcceptedMenu, &first) || draft.Lapsed {
		t.Fatalf("draft replaced the accepted menu: saved=%+v accepted=%+v lapsed=%v", draft.SavedMenu, draft.AcceptedMenu, draft.Lapsed)
	}
	if len(f.api.menuPublishes) != 1 {
		t.Fatal("saving a later draft called WeChat")
	}

	again, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 13})
	if err != nil || again == nil || again.Lapsed || !sameMenu(again.SavedMenu, &second) || !sameMenu(again.AcceptedMenu, &second) {
		t.Fatalf("second publish = (%v, %v)", again, err)
	}
	if len(f.api.menuPublishes) != 2 || !sameMenu(&f.api.menuPublishes[1].Menu, &second) {
		t.Fatalf("second call = %+v", f.api.menuPublishes)
	}
	got, err := f.apps.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !sameMenu(got.AcceptedMenu, &second) || !sameMenu(got.SavedMenu, &second) || got.Lapsed || got.UpdateBy != 13 {
		t.Fatalf("reread saved=%+v accepted=%+v lapsed=%v update_by=%d", got.SavedMenu, got.AcceptedMenu, got.Lapsed, got.UpdateBy)
	}
}

func TestRefusedOrTimedOutPublishKeepsTheAcceptedMenu(t *testing.T) {
	classes := []CallClass{CallBusinessRejection, CallUnavailable}
	for _, class := range classes {
		f := newAuthFixture(t)
		reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		accepted := entity.CustomMenu{Buttons: []entity.MenuButton{{
			Name: "官网", WebURL: "https://example.com/home",
		}}}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: accepted, ActorID: 11}); err != nil {
			t.Fatalf("save accepted: %v", err)
		}
		f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
		if _, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 11}); err != nil {
			t.Fatalf("class %d first publish: %v", class, err)
		}
		draft := entity.CustomMenu{Buttons: []entity.MenuButton{{
			Name: "新官网", WebURL: "https://example.com/new",
		}}}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: draft, ActorID: 12}); err != nil {
			t.Fatalf("save draft: %v", err)
		}
		f.api.menuPublish = PublishMenuResult{Class: class, Message: "这次没有受理"}
		published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 13})
		if err == nil || published != nil {
			t.Fatalf("class %d publish = (%v, %v), want a refusal", class, published, err)
		}
		got, err := f.apps.Get(context.Background(), reveal.App.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Lapsed || !sameMenu(got.AcceptedMenu, &accepted) || !sameMenu(got.SavedMenu, &draft) {
			t.Fatalf("class %d changed the live menu: saved=%+v accepted=%+v lapsed=%v", class, got.SavedMenu, got.AcceptedMenu, got.Lapsed)
		}
		if len(f.api.menuPublishes) != 2 || !sameMenu(&f.api.menuPublishes[1].Menu, &draft) {
			t.Fatalf("class %d call = %+v", class, f.api.menuPublishes)
		}
	}
}

func TestPublishDefinitiveRefusalLapsesAndKeepsTheAcceptedMenu(t *testing.T) {
	f := newAuthFixture(t)
	reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	accepted := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name: "官网", WebURL: "https://example.com/home",
	}}}
	if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: accepted, ActorID: 11}); err != nil {
		t.Fatalf("save accepted: %v", err)
	}
	f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
	if _, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 11}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	draft := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name: "新官网", WebURL: "https://example.com/new",
	}}}
	if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: draft, ActorID: 12}); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	f.api.menuPublish = PublishMenuResult{Class: CallDefinitiveRefusal, Message: "授权已被取消"}
	published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 13})
	if err == nil || published != nil {
		t.Fatalf("definitive refusal = (%v, %v)", published, err)
	}
	got, err := f.apps.Get(context.Background(), reveal.App.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Lapsed || got.UpdateBy != 0 || !sameMenu(got.AcceptedMenu, &accepted) || !sameMenu(got.SavedMenu, &draft) {
		t.Fatalf("after refusal saved=%+v accepted=%+v lapsed=%v update_by=%d", got.SavedMenu, got.AcceptedMenu, got.Lapsed, got.UpdateBy)
	}
}

func TestSubscriptionAndMiniProgramRefuseMenuLocally(t *testing.T) {
	f := newAuthFixture(t)
	subscription := serviceHold("wxsub000000000001", 7)
	subscription.Name = "订阅号乙"
	subscription.Kind = entity.AccountKindSubscription
	holds := []CreateDirectHoldRequest{subscription, miniHold("wxminiappid000001", 7)}
	menu := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name: "官网", WebURL: "https://example.com/home",
	}}}
	for _, hold := range holds {
		reveal, err := f.apps.Create(context.Background(), hold)
		if err != nil {
			t.Fatalf("create %s: %v", hold.AppID, err)
		}
		saved, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menu, ActorID: 11})
		if !errors.Is(err, wechat.ErrCapabilityUnavailable) || saved != nil {
			t.Fatalf("save %s = (%v, %v)", hold.AppID, saved, err)
		}
		published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 11})
		if !errors.Is(err, wechat.ErrCapabilityUnavailable) || published != nil {
			t.Fatalf("publish %s = (%v, %v)", hold.AppID, published, err)
		}
		got, err := f.apps.Get(context.Background(), reveal.App.ID)
		if err != nil {
			t.Fatalf("get %s: %v", hold.AppID, err)
		}
		if got.Lapsed || got.SavedMenu != nil || got.AcceptedMenu != nil {
			t.Fatalf("%s after refusal saved=%+v accepted=%+v lapsed=%v", hold.AppID, got.SavedMenu, got.AcceptedMenu, got.Lapsed)
		}
	}
	if len(f.api.menuPublishes) != 0 || len(f.api.appProbes) != 0 {
		t.Fatal("a local menu refusal called WeChat")
	}
}

func TestPlatformAuthorizationWithoutMenuCapabilityIsRefusedLocally(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxsvc000000000099",
		Name:         "无菜单服务号",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityTemplateMessage, entity.CapabilityWebAuthorization},
		RefreshToken: "authorizer-refresh-RRRR",
	})
	app, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-no-menu",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	menu := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name: "官网", WebURL: "https://example.com/home",
	}}}
	saved, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: app.ID, Menu: menu, ActorID: 11})
	if !errors.Is(err, wechat.ErrCapabilityUnavailable) || saved != nil {
		t.Fatalf("save = (%v, %v)", saved, err)
	}
	published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: app.ID, ActorID: 11})
	if !errors.Is(err, wechat.ErrCapabilityUnavailable) || published != nil {
		t.Fatalf("publish = (%v, %v)", published, err)
	}
	got, err := f.apps.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Lapsed || got.SavedMenu != nil || got.AcceptedMenu != nil {
		t.Fatalf("local refusal changed the app: saved=%+v accepted=%+v lapsed=%v", got.SavedMenu, got.AcceptedMenu, got.Lapsed)
	}
	if len(f.api.menuPublishes) != 0 || len(f.api.appProbes) != 0 {
		t.Fatal("a missing menu capability called WeChat")
	}
}

func TestSuspendedLapsedUnreadyAndWithdrawnAppsCannotPublish(t *testing.T) {
	menuA := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name: "官网", WebURL: "https://example.com/home",
	}}}
	menuB := entity.CustomMenu{Buttons: []entity.MenuButton{{
		Name: "新官网", WebURL: "https://example.com/new",
	}}}

	t.Run("suspended", func(t *testing.T) {
		f := newAuthFixture(t)
		reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menuA, ActorID: 11}); err != nil {
			t.Fatalf("save: %v", err)
		}
		f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
		if _, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 11}); err != nil {
			t.Fatalf("publish: %v", err)
		}
		if _, err := f.apps.SetSuspended(context.Background(), SetSuspendedRequest{ID: reveal.App.ID, Suspended: true, ActorID: 8}); err != nil {
			t.Fatalf("suspend: %v", err)
		}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menuB, ActorID: 12}); err != nil {
			t.Fatalf("save while suspended: %v", err)
		}
		published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 13})
		if !errors.Is(err, wechat.ErrAppSuspended) || published != nil {
			t.Fatalf("suspended publish = (%v, %v)", published, err)
		}
		got, err := f.apps.Get(context.Background(), reveal.App.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !got.Suspended || got.Lapsed || !sameMenu(got.SavedMenu, &menuB) || !sameMenu(got.AcceptedMenu, &menuA) {
			t.Fatalf("suspended publish changed the live menu: %+v accepted %+v", got.SavedMenu, got.AcceptedMenu)
		}
		if len(f.api.menuPublishes) != 1 {
			t.Fatalf("publishes = %d, want only the one before suspension", len(f.api.menuPublishes))
		}
	})

	t.Run("lapsed", func(t *testing.T) {
		f := newAuthFixture(t)
		reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menuA, ActorID: 11}); err != nil {
			t.Fatalf("save: %v", err)
		}
		f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
		if _, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 11}); err != nil {
			t.Fatalf("publish: %v", err)
		}
		f.api.appProbe = ProbeAppResult{Class: CallDefinitiveRefusal, Message: "授权已被取消"}
		if _, err := f.apps.Probe(context.Background(), reveal.App.ID); err != nil {
			t.Fatalf("lapse: %v", err)
		}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menuB, ActorID: 12}); err != nil {
			t.Fatalf("save while lapsed: %v", err)
		}
		published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 13})
		if !errors.Is(err, wechat.ErrAppLapsed) || published != nil {
			t.Fatalf("lapsed publish = (%v, %v)", published, err)
		}
		got, err := f.apps.Get(context.Background(), reveal.App.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !got.Lapsed || !sameMenu(got.SavedMenu, &menuB) || !sameMenu(got.AcceptedMenu, &menuA) {
			t.Fatalf("lapsed publish changed the live menu: saved=%+v accepted=%+v lapsed=%v", got.SavedMenu, got.AcceptedMenu, got.Lapsed)
		}
		if len(f.api.menuPublishes) != 1 {
			t.Fatalf("publishes = %d, want only the one before the lapse", len(f.api.menuPublishes))
		}
	})

	t.Run("unready platform", func(t *testing.T) {
		f := newAuthFixture(t)
		platform := f.holdReady(t, "华东平台", "wxcompappid000001")
		f.script(AuthorizationGrant{
			AppID:        "wxsvc000000000099",
			Name:         "服务号甲",
			AppType:      entity.AppTypeOfficialAccount,
			Kind:         entity.AccountKindService,
			Capabilities: []string{entity.CapabilityCustomMenu},
			RefreshToken: "authorizer-refresh-RRRR",
		})
		app, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
			PlatformID: platform.ID,
			AuthCode:   "auth-code-menu",
		})
		if err != nil {
			t.Fatalf("grant: %v", err)
		}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: app.ID, Menu: menuA, ActorID: 11}); err != nil {
			t.Fatalf("save: %v", err)
		}
		if _, err := f.platforms.ReleaseVerifyTicket(context.Background(), platform.ID); err != nil {
			t.Fatalf("release ticket: %v", err)
		}
		f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
		published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: app.ID, ActorID: 11})
		if !errors.Is(err, wechat.ErrPlatformNotReady) || published != nil {
			t.Fatalf("unready publish = (%v, %v)", published, err)
		}
		got, err := f.apps.Get(context.Background(), app.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Lapsed || !sameMenu(got.SavedMenu, &menuA) || got.AcceptedMenu != nil {
			t.Fatalf("unready publish changed the app: saved=%+v accepted=%+v lapsed=%v", got.SavedMenu, got.AcceptedMenu, got.Lapsed)
		}
		if len(f.api.menuPublishes) != 0 {
			t.Fatal("an unready platform was asked to publish")
		}
	})

	t.Run("withdrawn", func(t *testing.T) {
		f := newAuthFixture(t)
		reveal, err := f.apps.Create(context.Background(), serviceHold("wxsvc000000000001", 7))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menuA, ActorID: 11}); err != nil {
			t.Fatalf("save: %v", err)
		}
		if err := f.apps.Withdraw(context.Background(), WithdrawAppRequest{ID: reveal.App.ID, ActorID: 9}); err != nil {
			t.Fatalf("withdraw: %v", err)
		}
		saved, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: reveal.App.ID, Menu: menuB, ActorID: 12})
		if !errors.Is(err, wechat.ErrAppNotFound) || saved != nil {
			t.Fatalf("save withdrawn = (%v, %v)", saved, err)
		}
		published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: reveal.App.ID, ActorID: 12})
		if !errors.Is(err, wechat.ErrAppNotFound) || published != nil {
			t.Fatalf("publish withdrawn = (%v, %v)", published, err)
		}
		if len(f.api.menuPublishes) != 0 {
			t.Fatal("a withdrawn app called WeChat")
		}
	})
}

func TestPlatformAuthorizationPublishesTheWholeSavedMenu(t *testing.T) {
	f := newAuthFixture(t)
	platform := f.holdReady(t, "华东平台", "wxcompappid000001")
	f.script(AuthorizationGrant{
		AppID:        "wxsvc000000000099",
		Name:         "服务号甲",
		AppType:      entity.AppTypeOfficialAccount,
		Kind:         entity.AccountKindService,
		Capabilities: []string{entity.CapabilityCustomMenu, entity.CapabilityTemplateMessage},
		RefreshToken: "authorizer-refresh-RRRR",
	})
	app, err := f.apps.CompleteAuthorization(context.Background(), CompleteAuthorizationRequest{
		PlatformID: platform.ID,
		AuthCode:   "auth-code-menu",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	menu := entity.CustomMenu{Buttons: []entity.MenuButton{
		{Name: "官网", WebURL: "https://example.com/home"},
		{Name: "更多", SubButtons: []entity.MenuButton{
			{Name: "商城", MiniProgramAppID: "wxminiappid000001", MiniProgramPage: "pages/mall"},
		}},
	}}
	if _, err := f.apps.SaveMenu(context.Background(), SaveMenuRequest{ID: app.ID, Menu: menu, ActorID: 11}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(f.api.menuPublishes) != 0 {
		t.Fatal("saving a platform menu called WeChat")
	}
	f.api.menuPublish = PublishMenuResult{Class: CallSuccess, Message: "已替换默认菜单"}
	published, err := f.apps.PublishMenu(context.Background(), PublishMenuRequest{ID: app.ID, ActorID: 11})
	if err != nil || published == nil || published.Lapsed {
		t.Fatalf("publish = (%v, %v)", published, err)
	}
	if !sameMenu(published.SavedMenu, &menu) || !sameMenu(published.AcceptedMenu, &menu) || published.UpdateBy != 11 {
		t.Fatalf("published saved=%+v accepted=%+v update_by=%d", published.SavedMenu, published.AcceptedMenu, published.UpdateBy)
	}
	if len(f.api.menuPublishes) != 1 || !sameMenu(&f.api.menuPublishes[0].Menu, &menu) || f.api.menuPublishes[0].AppID != app.AppID || f.api.menuPublishes[0].Connection != entity.ConnectionPlatformAuthorization {
		t.Fatalf("publish call = %+v", f.api.menuPublishes)
	}
	got, err := f.apps.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Lapsed || !sameMenu(got.AcceptedMenu, &menu) {
		t.Fatalf("reread accepted=%+v lapsed=%v", got.AcceptedMenu, got.Lapsed)
	}
}

func serviceHold(appID string, actorID int64) CreateDirectHoldRequest {
	return CreateDirectHoldRequest{
		Name:      "服务号甲",
		AppID:     appID,
		AppType:   entity.AppTypeOfficialAccount,
		Kind:      entity.AccountKindService,
		AppSecret: testAppSecret,
		ActorID:   actorID,
	}
}

func sameMenu(got, want *entity.CustomMenu) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return sameButtons(got.Buttons, want.Buttons)
}

func sameButtons(got, want []entity.MenuButton) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].WebURL != want[i].WebURL || got[i].MiniProgramAppID != want[i].MiniProgramAppID || got[i].MiniProgramPage != want[i].MiniProgramPage {
			return false
		}
		if !sameButtons(got[i].SubButtons, want[i].SubButtons) {
			return false
		}
	}
	return true
}
