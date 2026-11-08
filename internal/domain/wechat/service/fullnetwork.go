package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// fullNetworkAccounts are the dedicated test accounts WeChat publishes for
// 全网发布检测. They are WeChat's identifiers, not this product's, and they
// are not a frozen requirement of the spec. A push from any other account
// is an ordinary verified event.
var fullNetworkAccounts = map[string]struct{}{
	"wx570bc396a51b8ff8": {}, "gh_3c884a361561": {},
	"wx9252c5e0bb1836fc": {}, "gh_c0f28a78b318": {},
	"wx8e1097c5bc82cde9": {}, "gh_3f222ed8d140": {},
	"wx14550af28c71a144": {}, "gh_26128078e9ab": {},
	"wxa35b9c23cfe664eb": {}, "gh_2b3713f184a6": {},
	"wxd101a85aa106f53e": {}, "gh_8dad206e9538": {},
	"wxc39235c15087f6f3": {}, "gh_905ae9d01059": {},
	"wx7720d01d4b2a4500": {}, "gh_393666f1fdf4": {},
	"wx05d483572dcd5d8b": {}, "gh_39abb5d4e1b7": {},
	"wx5910277cae6fd970": {}, "gh_7818dcb60240": {},
}

const fullNetworkTextCallback = "TESTCOMPONENT_MSG_TYPE_TEXT_callback"

func fullNetworkAccount(toUserName, authorizerAppID string) bool {
	if _, ok := fullNetworkAccounts[toUserName]; ok {
		return true
	}
	_, ok := fullNetworkAccounts[authorizerAppID]
	return ok
}

func (s *platformImpl) answerFullNetwork(ctx context.Context, p *entity.Platform, secrets repository.Secrets, req PushRequest, msg inboundPush) (*PushOutcome, bool, error) {
	if !fullNetworkAccount(msg.ToUserName, req.AuthorizerAppID) {
		return nil, false, nil
	}
	if msg.MsgType == "text" && strings.HasPrefix(msg.Content, "QUERY_AUTH_CODE:") {
		s.deliverFullNetworkQuery(ctx, p, secrets, msg)
		return &PushOutcome{Body: "", ContentType: "text/plain; charset=utf-8"}, true, nil
	}
	if msg.MsgType == "text" && msg.Content == "TESTCOMPONENT_MSG_TYPE_TEXT" {
		plain := textReplyXML(msg.FromUserName, msg.ToUserName, fullNetworkTextCallback)
		sealed, err := s.pushes.SealReply(ctx, SealReplyRequest{
			Token:          secrets.Token,
			EncodingAESKey: secrets.EncodingAESKey,
			ComponentAppID: p.ComponentAppID,
			Nonce:          req.Nonce,
			Plaintext:      plain,
		})
		if err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.receive", err)
			return nil, true, err
		}
		return &PushOutcome{Body: sealed, ContentType: "application/xml; charset=utf-8"}, true, nil
	}
	return nil, false, nil
}

func (s *platformImpl) deliverFullNetworkQuery(ctx context.Context, p *entity.Platform, secrets repository.Secrets, msg inboundPush) {
	code := strings.TrimPrefix(msg.Content, "QUERY_AUTH_CODE:")
	if code == "" || secrets.ComponentSecret == "" || msg.FromUserName == "" {
		return
	}
	ticket, err := s.platforms.LoadVerifyTicket(ctx, p.ID)
	if err != nil || ticket == "" || s.openapi == nil {
		s.warnFullNetwork(ctx, p.ID, err)
		return
	}
	err = s.openapi.AnswerFullNetwork(ctx, FullNetworkReply{
		ComponentAppID:  p.ComponentAppID,
		ComponentSecret: secrets.ComponentSecret,
		VerifyTicket:    ticket,
		QueryAuthCode:   code,
		ToUser:          msg.FromUserName,
		Content:         code + "_from_api",
	})
	if err != nil {
		// The callback itself is the empty body WeChat requires. This
		// follow-up is the published customer-service step; its failure
		// does not change the platform and does not fail the callback.
		s.warnFullNetwork(ctx, p.ID, err)
	}
}

func (s *platformImpl) warnFullNetwork(ctx context.Context, platformID int64, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	s.logger.WarnContext(ctx, "full-network release reply was not delivered",
		slog.String("operation", "wechat.svc.platform.receive"),
		slog.Int64("platform_id", platformID),
		slog.Any("error", err))
}

func textReplyXML(toUser, fromUser, content string) string {
	return "<xml><ToUserName><![CDATA[" + xmlCDATA(toUser) + "]]></ToUserName>" +
		"<FromUserName><![CDATA[" + xmlCDATA(fromUser) + "]]></FromUserName>" +
		"<CreateTime>" + replyTimestamp() + "</CreateTime>" +
		"<MsgType><![CDATA[text]]></MsgType>" +
		"<Content><![CDATA[" + xmlCDATA(content) + "]]></Content></xml>"
}
