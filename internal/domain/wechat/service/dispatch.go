package service

import (
	"context"
	"errors"
	"strings"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// AdoptTemplateRequest takes one public-library template onto one live
// app. Channel names which library the template fills. The actor is
// stamped into the audit fields.
type AdoptTemplateRequest struct {
	AppID             int64
	LibraryTemplateID string
	Channel           string
	ActorID           int64
}

// refuseChannel rejects a send the app's kind cannot make, and a
// Platform Authorization that was not granted that capability. Direct
// Holds are gated by kind alone. The check is local.
func refuseChannel(app *entity.App, channel string) error {
	switch channel {
	case entity.CapabilityTemplateMessage, entity.CapabilitySubscribeNotification:
		if app.AppType != entity.AppTypeOfficialAccount || app.Kind != entity.AccountKindService {
			return wechat.ErrCapabilityDenied
		}
	case entity.CapabilitySubscribeMessage:
		if app.AppType != entity.AppTypeMiniProgram {
			return wechat.ErrCapabilityDenied
		}
	default:
		return wechat.ErrCapabilityDenied
	}
	if app.Connection != entity.ConnectionPlatformAuthorization {
		return nil
	}
	for _, granted := range app.Capabilities {
		if granted == channel {
			return nil
		}
	}
	return wechat.ErrCapabilityDenied
}

// refuseUnusable rejects adopt and send while the app is suspended,
// lapsed, or withdrawn. Withdrawn apps are already hidden by FindByID.
// An unready platform is a separate refusal and does not lapse the app.
func refuseUnusable(app *entity.App) error {
	if app.Suspended {
		return wechat.ErrAppSuspended
	}
	if app.Lapsed {
		return wechat.ErrAppLapsed
	}
	return nil
}

// AdoptTemplate calls WeChat with the app's current token and, when
// WeChat accepts, records one Adopted Template. A refusal records nothing.
func (s *appImpl) AdoptTemplate(ctx context.Context, req AdoptTemplateRequest) (*entity.AdoptedTemplate, error) {
	const op = "wechat.svc.template.adopt"
	app, err := s.apps.FindByID(ctx, req.AppID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := refuseUnusable(app); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	libraryID := strings.TrimSpace(req.LibraryTemplateID)
	existing, err := s.templates.FindByAppAndLibrary(ctx, app.ID, libraryID)
	if err != nil && !errors.Is(err, wechat.ErrTemplateNotFound) {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if existing != nil {
		err = wechat.ErrTemplateDuplicate
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	call, blocked, err := s.probeRequest(ctx, app)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if blocked != nil {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if s.openapi == nil {
		err = wechat.ErrWechatUnavailable
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	result, err := s.openapi.AdoptLibraryTemplate(ctx, AdoptLibraryTemplateRequest{
		ProbeAppRequest:   call,
		LibraryTemplateID: libraryID,
		Channel:           req.Channel,
	})
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, wechat.ErrWechatUnavailable
	}
	if result.Class == CallDefinitiveRefusal {
		if err := s.lapse(ctx, app); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		err = wechat.ErrAppLapsed
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if result.Class != CallSuccess {
		if result.Class == CallUnavailable {
			err = wechat.ErrWechatUnavailable
		} else {
			err = wechat.ErrTemplateRejected
		}
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := s.keepAuthorizerRefresh(ctx, app, result.AuthorizerRefreshToken); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	row := &entity.AdoptedTemplate{
		ID:                s.templates.NextID(),
		AppID:             app.ID,
		LibraryTemplateID: libraryID,
		WechatTemplateID:  result.WechatTemplateID,
		Title:             result.Title,
		Channel:           req.Channel,
		CreateBy:          req.ActorID,
		UpdateBy:          req.ActorID,
	}
	created, err := s.templates.Create(ctx, row)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return created, nil
}

// SendDispatchRequest is one synchronous send of an adopted template to
// one openid. The actor is stamped into the audit fields. A blank openid
// or a template this app has not adopted creates no dispatch.
type SendDispatchRequest struct {
	AppID           int64
	TemplateID      int64
	OpenID          string
	Content         string
	WebURL          string
	MiniProgramPage string
	ActorID         int64
}

// ListDispatchesRequest selects WeChat Dispatches. AppID 0 lists every
// attempt, including those whose app has been withdrawn. Limit 0 returns
// the whole selection.
type ListDispatchesRequest struct {
	AppID  int64
	Limit  int
	Offset int
}

// DispatchPage is one page of WeChat Dispatches.
type DispatchPage struct {
	Dispatches []*entity.Dispatch
}

// Send waits until WeChat accepts or rejects. Acceptance records an
// accepted dispatch. Anything else is filled in by later tests.
func (s *appImpl) Send(ctx context.Context, req SendDispatchRequest) (*entity.Dispatch, error) {
	const op = "wechat.svc.dispatch.send"
	app, err := s.apps.FindByID(ctx, req.AppID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := refuseUnusable(app); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	tpl, err := s.templates.FindByID(ctx, req.TemplateID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if tpl.AppID != app.ID {
		err = wechat.ErrTemplateNotFound
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if strings.TrimSpace(req.OpenID) == "" {
		err = wechat.ErrDispatchInvalid
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := refuseChannel(app, tpl.Channel); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	call, blocked, err := s.probeRequest(ctx, app)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if blocked != nil {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	result, err := s.openapi.SendMessage(ctx, SendMessageRequest{
		ProbeAppRequest:  call,
		Channel:          tpl.Channel,
		OpenID:           strings.TrimSpace(req.OpenID),
		WechatTemplateID: tpl.WechatTemplateID,
		Content:          req.Content,
		WebURL:           strings.TrimSpace(req.WebURL),
		MiniProgramPage:  strings.TrimSpace(req.MiniProgramPage),
	})
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, wechat.ErrWechatUnavailable
	}
	if result.Class == CallDefinitiveRefusal {
		if err := s.lapse(ctx, app); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		err = wechat.ErrAppLapsed
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if result.Class == CallBusinessRejection {
		created, err := s.recordDispatch(ctx, req, app, tpl, false, "")
		if err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		err = wechat.ErrDispatchRejected
		logx.Propagate(ctx, s.logger, op, err)
		return created, err
	}
	if result.Class != CallSuccess {
		err = wechat.ErrWechatUnavailable
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := s.keepAuthorizerRefresh(ctx, app, result.AuthorizerRefreshToken); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	created, err := s.recordDispatch(ctx, req, app, tpl, true, result.MsgID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return created, nil
}

func (s *appImpl) recordDispatch(ctx context.Context, req SendDispatchRequest, app *entity.App, tpl *entity.AdoptedTemplate, accepted bool, msgid string) (*entity.Dispatch, error) {
	return s.dispatches.Create(ctx, &entity.Dispatch{
		ID:                s.dispatches.NextID(),
		AppID:             app.ID,
		OpenID:            strings.TrimSpace(req.OpenID),
		TemplateID:        tpl.ID,
		LibraryTemplateID: tpl.LibraryTemplateID,
		WechatTemplateID:  tpl.WechatTemplateID,
		Channel:           tpl.Channel,
		Content:           req.Content,
		WebURL:            strings.TrimSpace(req.WebURL),
		MiniProgramPage:   strings.TrimSpace(req.MiniProgramPage),
		Accepted:          accepted,
		MsgID:             msgid,
		CreateBy:          req.ActorID,
		UpdateBy:          req.ActorID,
	})
}

// RecordDelivery writes a delivery report onto the attempt that carries
// this msgid. WeChat caused the write, so the actor is the system. A
// blank or unknown msgid leaves every dispatch as it was.
func (s *appImpl) RecordDelivery(ctx context.Context, msgid string) error {
	const op = "wechat.svc.dispatch.deliver"
	msgid = strings.TrimSpace(msgid)
	if msgid == "" || s.dispatches == nil {
		return nil
	}
	if err := s.dispatches.MarkDelivered(ctx, repository.MarkDeliveredRequest{
		MsgID:   msgid,
		ActorID: systemActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return err
	}
	return nil
}

// ListDispatches returns the attempts an operator can review. AppID 0
// does not hide dispatches whose app was withdrawn.
func (s *appImpl) ListDispatches(ctx context.Context, req ListDispatchesRequest) (*DispatchPage, error) {
	const op = "wechat.svc.dispatch.list"
	rows, err := s.dispatches.List(ctx, repository.DispatchQuery{AppID: req.AppID, Limit: req.Limit, Offset: req.Offset})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return &DispatchPage{Dispatches: rows}, nil
}

// ListTemplates returns templates adopted onto this app. Withdrawal of
// the app does not remove them.
func (s *appImpl) ListTemplates(ctx context.Context, appID int64) ([]*entity.AdoptedTemplate, error) {
	const op = "wechat.svc.template.list"
	rows, err := s.templates.ListByApp(ctx, appID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return rows, nil
}
