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
	AuthorizerAppid       string `xml:"AuthorizerAppid"`
	AuthorizationCode     string `xml:"AuthorizationCode"`
	ToUserName            string `xml:"ToUserName"`
	FromUserName          string `xml:"FromUserName"`
	MsgType               string `xml:"MsgType"`
	Content               string `xml:"Content"`
	Event                 string `xml:"Event"`
	MsgID                 string `xml:"MsgID"`
	// Lists is the mini-program form of a subscribe-message delivery.
	// The msgid is nested, not a top-level MsgID.
	Lists []struct {
		MsgID string `xml:"MsgID"`
	} `xml:"List"`
	// SubscribeMsgSentEvent is the official-account form of that same
	// delivery. The msgid is nested one level further down.
	SubscribeMsgSentEvent struct {
		Lists []struct {
			MsgID string `xml:"MsgID"`
		} `xml:"List"`
	} `xml:"SubscribeMsgSentEvent"`
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
	if msg.InfoType == "authorized" || msg.InfoType == "updateauthorized" {
		return s.completePushedAuthorization(ctx, p, msg)
	}
	if outcome, handled, err := s.answerFullNetwork(ctx, p, secrets, req, msg); handled || err != nil {
		return outcome, err
	}
	if msgid, ok := deliveryMsgID(msg); ok && s.bindings != nil {
		if err := s.bindings.RecordDelivery(ctx, msgid); err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
			return nil, err
		}
	}
	return successOutcome(), nil
}

// deliveryMsgID reports the msgid of a delivery report. Follow, text, and
// other events are not delivery reports, even when they carry a message id.
func deliveryMsgID(msg inboundPush) (string, bool) {
	switch strings.TrimSpace(msg.Event) {
	case "TEMPLATESENDJOBFINISH", "subscribe_msg_sent_event":
	default:
		return "", false
	}
	id := strings.TrimSpace(msg.MsgID)
	if id == "" {
		for _, item := range msg.Lists {
			id = strings.TrimSpace(item.MsgID)
			if id != "" {
				break
			}
		}
	}
	if id == "" {
		for _, item := range msg.SubscribeMsgSentEvent.Lists {
			id = strings.TrimSpace(item.MsgID)
			if id != "" {
				break
			}
		}
	}
	if id == "" {
		return "", false
	}
	return id, true
}

// completePushedAuthorization turns WeChat's authorized push into the
// grant the app service records. The authorization code is exchanged by
// the Open API port; this method does not read a permission-set number.
// A missing code is acknowledged and stores nothing. An unauthorized
// push is not handled here — lapse belongs to a later ticket.
func (s *platformImpl) completePushedAuthorization(ctx context.Context, p *entity.Platform, msg inboundPush) (*PushOutcome, error) {
	code := strings.TrimSpace(msg.AuthorizationCode)
	if code == "" || s.bindings == nil {
		return successOutcome(), nil
	}
	if _, err := s.bindings.CompleteAuthorization(ctx, CompleteAuthorizationRequest{
		PlatformID: p.ID,
		AuthCode:   code,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
		return nil, err
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

// AppPushRequest is one callback delivery for a Direct Hold. ID is the
// app row. The Push Secret on that row authenticates the body.
type AppPushRequest struct {
	ID           int64
	Timestamp    string
	Nonce        string
	Signature    string
	MsgSignature string
	EncryptType  string
	Body         []byte
}

// Receive accepts one encrypted push on a Direct Hold callback. A
// delivery report updates the matching dispatch. Anything else that
// verifies is acknowledged and stored nowhere, with no automatic reply.
func (s *appImpl) Receive(ctx context.Context, req AppPushRequest) (*PushOutcome, error) {
	const op = "wechat.svc.app.receive"
	app, err := s.apps.FindByIDIncludingWithdrawn(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if app.Connection != entity.ConnectionDirectHold {
		err = wechat.ErrPushRejected
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	secrets, err := s.apps.LoadSecretsIncludingWithdrawn(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	authed, err := s.pushes.Authenticate(ctx, AuthenticateRequest{
		Token:          secrets.Token,
		EncodingAESKey: secrets.EncodingAESKey,
		ComponentAppID: app.AppID,
		Timestamp:      req.Timestamp,
		Nonce:          req.Nonce,
		Signature:      req.Signature,
		MsgSignature:   req.MsgSignature,
		EncryptType:    req.EncryptType,
		Body:           req.Body,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	var msg inboundPush
	if err := xml.Unmarshal(authed.XML, &msg); err != nil {
		logx.Propagate(ctx, s.logger, op, wechat.ErrPushRejected)
		return nil, wechat.ErrPushRejected
	}
	if msgid, ok := deliveryMsgID(msg); ok {
		if err := s.RecordDelivery(ctx, msgid); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
	}
	return successOutcome(), nil
}
