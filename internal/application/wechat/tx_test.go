package wechat

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	wechatv1 "glex/api/wechat/v1"
	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
)

type txJournalKey struct{}

type recordingTx struct {
	began     int
	committed []string
}

func (t *recordingTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	t.began++
	journal := []string{}
	err := fn(context.WithValue(ctx, txJournalKey{}, &journal))
	if err != nil {
		return err
	}
	t.committed = append(t.committed, journal...)
	return nil
}

func noteWrite(ctx context.Context, name string) {
	journal, _ := ctx.Value(txJournalKey{}).(*[]string)
	if journal == nil {
		return
	}
	*journal = append(*journal, name)
}

var errSecondWrite = errors.New("second write")

type recordingApps struct {
	wechatservice.App
	failSecond bool
	reject     bool
	lapse      bool
}

func (a *recordingApps) AdoptTemplate(ctx context.Context, req wechatservice.AdoptTemplateRequest) (*entity.AdoptedTemplate, error) {
	noteWrite(ctx, "authorizer-refresh")
	if a.failSecond {
		return nil, errSecondWrite
	}
	if a.lapse {
		noteWrite(ctx, "lapse")
		return nil, wechat.ErrAppLapsed
	}
	noteWrite(ctx, "template")
	return &entity.AdoptedTemplate{ID: 11, AppID: req.AppID, Title: "订单"}, nil
}

func (a *recordingApps) PublishMenu(ctx context.Context, req wechatservice.PublishMenuRequest) (*entity.App, error) {
	noteWrite(ctx, "authorizer-refresh")
	if a.failSecond {
		return nil, errSecondWrite
	}
	noteWrite(ctx, "accepted-menu")
	return &entity.App{ID: req.ID}, nil
}

func (a *recordingApps) Send(ctx context.Context, req wechatservice.SendDispatchRequest) (*entity.Dispatch, error) {
	if a.reject {
		noteWrite(ctx, "dispatch")
		return &entity.Dispatch{ID: 21, OpenID: req.OpenID}, wechat.ErrDispatchRejected
	}
	noteWrite(ctx, "authorizer-refresh")
	if a.failSecond {
		return nil, errSecondWrite
	}
	noteWrite(ctx, "dispatch")
	return &entity.Dispatch{ID: 21, AppID: req.AppID, OpenID: req.OpenID, Accepted: true}, nil
}

func TestGrantWritesStayAtomic(t *testing.T) {
	t.Run("adopt rolls back the refresh when the template write fails", func(t *testing.T) {
		tx := &recordingTx{}
		app := NewWechatApplication(nil, &recordingApps{failSecond: true}, tx, slog.New(slog.DiscardHandler))
		_, err := app.AdoptWechatTemplate(context.Background(), &wechatv1.AdoptWechatTemplateRequest{
			AppId:             1,
			LibraryTemplateId: "TM00015",
			Channel:           wechatv1.WechatCapability_WECHAT_CAPABILITY_TEMPLATE_MESSAGE,
		})
		if !errors.Is(err, errSecondWrite) {
			t.Fatalf("adopt = %v", err)
		}
		if tx.began != 1 || len(tx.committed) != 0 {
			t.Fatalf("tx began %d committed %v", tx.began, tx.committed)
		}
	})

	t.Run("publish rolls back the refresh when the accepted menu write fails", func(t *testing.T) {
		tx := &recordingTx{}
		app := NewWechatApplication(nil, &recordingApps{failSecond: true}, tx, slog.New(slog.DiscardHandler))
		_, err := app.PublishWechatAppMenu(context.Background(), &wechatv1.PublishWechatAppMenuRequest{AppId: 1})
		if !errors.Is(err, errSecondWrite) {
			t.Fatalf("publish = %v", err)
		}
		if tx.began != 1 || len(tx.committed) != 0 {
			t.Fatalf("tx began %d committed %v", tx.began, tx.committed)
		}
	})

	t.Run("send rolls back the refresh when the dispatch write fails", func(t *testing.T) {
		tx := &recordingTx{}
		app := NewWechatApplication(nil, &recordingApps{failSecond: true}, tx, slog.New(slog.DiscardHandler))
		_, err := app.CreateWechatDispatch(context.Background(), &wechatv1.CreateWechatDispatchRequest{
			AppId: 1, TemplateId: 2, Openid: "openid-a",
		})
		if !errors.Is(err, errSecondWrite) {
			t.Fatalf("send = %v", err)
		}
		if tx.began != 1 || len(tx.committed) != 0 {
			t.Fatalf("tx began %d committed %v", tx.began, tx.committed)
		}
	})

	t.Run("a rejected dispatch stays committed", func(t *testing.T) {
		tx := &recordingTx{}
		app := NewWechatApplication(nil, &recordingApps{reject: true}, tx, slog.New(slog.DiscardHandler))
		_, err := app.CreateWechatDispatch(context.Background(), &wechatv1.CreateWechatDispatchRequest{
			AppId: 1, TemplateId: 2, Openid: "openid-a",
		})
		if !errors.Is(err, wechat.ErrDispatchRejected) {
			t.Fatalf("send = %v", err)
		}
		if tx.began != 1 || len(tx.committed) != 1 || tx.committed[0] != "dispatch" {
			t.Fatalf("tx began %d committed %v", tx.began, tx.committed)
		}
	})

	t.Run("a lapse stays committed", func(t *testing.T) {
		tx := &recordingTx{}
		app := NewWechatApplication(nil, &recordingApps{lapse: true}, tx, slog.New(slog.DiscardHandler))
		_, err := app.AdoptWechatTemplate(context.Background(), &wechatv1.AdoptWechatTemplateRequest{
			AppId:             1,
			LibraryTemplateId: "TM00015",
			Channel:           wechatv1.WechatCapability_WECHAT_CAPABILITY_TEMPLATE_MESSAGE,
		})
		if !errors.Is(err, wechat.ErrAppLapsed) {
			t.Fatalf("adopt = %v", err)
		}
		if tx.began != 1 || len(tx.committed) != 2 || tx.committed[1] != "lapse" {
			t.Fatalf("tx began %d committed %v", tx.began, tx.committed)
		}
	})
}
