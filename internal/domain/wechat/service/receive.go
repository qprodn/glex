package service

import (
	"context"
	"encoding/xml"
	"strings"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// PushRequest is one callback delivery for a single platform: the raw
// query WeChat signs and the raw body. AuthorizerAppID is set when the
// message URL carried the authorized app's AppID; the authorization-event
// URL leaves it empty.
type PushRequest struct {
	PlatformID      int64
	AuthorizerAppID string
	Timestamp       string
	Nonce           string
	Signature       string
	MsgSignature    string
	EncryptType     string
	Body            []byte
}

// PushOutcome is what the callback writes back to WeChat. Body is "success"
// for a verify ticket and for an ordinary verified event, an encrypted
// reply for a full-network text case, and empty for the published
// query-auth-code case.
type PushOutcome struct {
	Body        string
	ContentType string
}

type inboundPush struct {
	AppID                 string `xml:"AppId"`
	InfoType              string `xml:"InfoType"`
	ComponentVerifyTicket string `xml:"ComponentVerifyTicket"`
	ToUserName            string `xml:"ToUserName"`
	FromUserName          string `xml:"FromUserName"`
	MsgType               string `xml:"MsgType"`
	Content               string `xml:"Content"`
}

func (s *platformImpl) Receive(ctx context.Context, req PushRequest) (*PushOutcome, error) {
	p, err := s.platforms.FindByID(ctx, req.PlatformID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
		return nil, err
	}
	secrets, err := s.platforms.LoadSecrets(ctx, req.PlatformID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
		return nil, err
	}
	authed, err := s.pushes.Authenticate(ctx, AuthenticateRequest{
		Token:          secrets.Token,
		EncodingAESKey: secrets.EncodingAESKey,
		ComponentAppID: p.ComponentAppID,
		Timestamp:      req.Timestamp,
		Nonce:          req.Nonce,
		Signature:      req.Signature,
		MsgSignature:   req.MsgSignature,
		EncryptType:    req.EncryptType,
		Body:           req.Body,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
		return nil, err
	}
	var msg inboundPush
	if err := xml.Unmarshal(authed.XML, &msg); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", wechat.ErrPushRejected)
		return nil, wechat.ErrPushRejected
	}
	return s.applyPush(ctx, p, secrets, req, msg)
}

func (s *platformImpl) applyPush(ctx context.Context, p *entity.Platform, secrets repository.Secrets, req PushRequest, msg inboundPush) (*PushOutcome, error) {
	if msg.InfoType == "component_verify_ticket" {
		return s.holdPushedTicket(ctx, p, msg)
	}
	if outcome, handled, err := s.answerFullNetwork(ctx, p, secrets, req, msg); handled || err != nil {
		return outcome, err
	}
	return successOutcome(), nil
}

func (s *platformImpl) holdPushedTicket(ctx context.Context, p *entity.Platform, msg inboundPush) (*PushOutcome, error) {
	ticket := strings.TrimSpace(msg.ComponentVerifyTicket)
	if ticket == "" || (msg.AppID != "" && msg.AppID != p.ComponentAppID) {
		return successOutcome(), nil
	}
	if _, err := s.platforms.HoldVerifyTicket(ctx, repository.HoldVerifyTicketRequest{
		ID:      p.ID,
		Ticket:  ticket,
		ActorID: systemActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
		return nil, err
	}
	return successOutcome(), nil
}

func successOutcome() *PushOutcome {
	return &PushOutcome{Body: "success", ContentType: "text/plain; charset=utf-8"}
}
