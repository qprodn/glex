package service

import (
	"context"

	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// ProbeOutcome is one WeChat Connectivity Test. OK is false when the
// platform cannot yield a token — missing credentials, no verify ticket,
// or WeChat refusing the call. The test changes no platform field an
// operator reads and no WeChat App.
type ProbeOutcome struct {
	OK      bool
	Message string
}

func (s *platformImpl) Probe(ctx context.Context, id int64) (*ProbeOutcome, error) {
	p, err := s.platforms.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.probe", err)
		return nil, err
	}
	secrets, err := s.platforms.LoadSecrets(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.probe", err)
		return nil, err
	}
	ticket, err := s.platforms.LoadVerifyTicket(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.probe", err)
		return nil, err
	}
	if secrets.ComponentSecret == "" || secrets.Token == "" || secrets.EncodingAESKey == "" || ticket == "" {
		return &ProbeOutcome{OK: false, Message: "平台未就绪"}, nil
	}
	err = s.openapi.ProbePlatform(ctx, ProbePlatformRequest{
		ComponentAppID:  p.ComponentAppID,
		ComponentSecret: secrets.ComponentSecret,
		VerifyTicket:    ticket,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.probe", err)
		return &ProbeOutcome{OK: false, Message: err.Error()}, nil
	}
	return &ProbeOutcome{OK: true, Message: "连接成功"}, nil
}

// ReleaseVerifyTicket records that the platform no longer holds a verify
// ticket. The stamp is the system itself: there is no operator button
// whose only job is deleting the ticket.
func (s *platformImpl) ReleaseVerifyTicket(ctx context.Context, id int64) (*entity.Platform, error) {
	p, err := s.platforms.ReleaseVerifyTicket(ctx, repository.ReleaseVerifyTicketRequest{
		ID:      id,
		ActorID: systemActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.release_verify_ticket", err)
		return nil, err
	}
	return s.present(p), nil
}
