package wechat

import (
	"context"

	wechatv1 "glex/api/wechat/v1"
	"glex/internal/bizpkg"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertPlatform maps a platform to the proto DTO. Only masks cross the
// wire — the secrets and the ciphertext stay server-side (ADR 0029).
func convertPlatform(p *entity.Platform) *wechatv1.WechatPlatform {
	return &wechatv1.WechatPlatform{
		PlatformId:          p.ID,
		Name:                p.Name,
		ComponentAppId:      p.ComponentAppID,
		ComponentSecretMask: p.ComponentSecretMask,
		TokenMask:           p.TokenMask,
		EncodingAesKeyMask:  p.EncodingAESKeyMask,
		KeyVersion:          int32(p.KeyVersion),
		CreateBy:            p.CreateBy,
		UpdateBy:            p.UpdateBy,
		CreatedAt:           timestamppb.New(p.CreatedAt),
		UpdatedAt:           timestamppb.New(p.UpdatedAt),
	}
}

// platformListSpec is this list's filterable idents and default order
// (ADR 0024).
var platformListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("name", filtering.TypeString),
		filtering.DeclareIdent("component_app_id", filtering.TypeString),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// CreateWechatPlatform registers a platform. create_by and update_by are
// stamped from the authenticated Principal; the request carries neither
// (ADR 0002). The response echoes masks, not the secrets just submitted.
func (s *WechatApplication) CreateWechatPlatform(ctx context.Context, req *wechatv1.CreateWechatPlatformRequest) (*wechatv1.WechatPlatform, error) {
	created, err := s.platforms.Create(ctx, wechatservice.CreateRequest{
		Name:            req.GetName(),
		ComponentAppID:  req.GetComponentAppId(),
		ComponentSecret: req.GetComponentSecret(),
		Token:           req.GetToken(),
		EncodingAESKey:  req.GetEncodingAesKey(),
		ActorID:         s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.create_platform", err)
		return nil, err
	}
	return convertPlatform(created), nil
}

// ListWechatPlatforms lists platforms that are not withdrawn.
func (s *WechatApplication) ListWechatPlatforms(ctx context.Context, req *wechatv1.ListWechatPlatformsRequest) (*wechatv1.WechatPlatformSet, error) {
	q, page, err := listq.Parse(req, platformListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_platforms", err)
		return nil, err
	}
	platforms, err := s.platforms.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_platforms", err)
		return nil, err
	}
	set := &wechatv1.WechatPlatformSet{Platforms: make([]*wechatv1.WechatPlatform, 0, len(platforms))}
	for _, p := range platforms {
		set.Platforms = append(set.Platforms, convertPlatform(p))
	}
	set.NextPageToken = page.NextToken(len(platforms))
	return set, nil
}

// GetWechatPlatform retrieves one platform that is not withdrawn.
func (s *WechatApplication) GetWechatPlatform(ctx context.Context, req *wechatv1.GetWechatPlatformRequest) (*wechatv1.WechatPlatform, error) {
	p, err := s.platforms.Get(ctx, req.GetPlatformId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.get_platform", err)
		return nil, err
	}
	return convertPlatform(p), nil
}

// UpdateWechatPlatform corrects the display name and replaces any secret
// the caller submitted. The component AppID cannot change.
func (s *WechatApplication) UpdateWechatPlatform(ctx context.Context, req *wechatv1.UpdateWechatPlatformRequest) (*wechatv1.WechatPlatform, error) {
	updated, err := s.platforms.Update(ctx, wechatservice.UpdateRequest{
		ID:              req.GetPlatformId(),
		Name:            req.GetName(),
		ComponentAppID:  req.GetComponentAppId(),
		ComponentSecret: req.GetComponentSecret(),
		Token:           req.GetToken(),
		EncodingAESKey:  req.GetEncodingAesKey(),
		ActorID:         s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.update_platform", err)
		return nil, err
	}
	return convertPlatform(updated), nil
}

// WithdrawWechatPlatform soft-deletes the platform.
func (s *WechatApplication) WithdrawWechatPlatform(ctx context.Context, req *wechatv1.WithdrawWechatPlatformRequest) (*emptypb.Empty, error) {
	if err := s.platforms.Withdraw(ctx, wechatservice.WithdrawRequest{
		ID:      req.GetPlatformId(),
		ActorID: s.actor(ctx),
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.withdraw_platform", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// SecretFields are the proto JSON names operlog scrubs from platform
// writes (ADR 0012, ADR 0029). Nil-tolerant so a wechat-less server
// assembly still boots.
func (s *WechatApplication) SecretFields() []string {
	if s == nil {
		return nil
	}
	return []string{"component_secret", "token", "encoding_aes_key"}
}

func (s *WechatApplication) actor(ctx context.Context) int64 {
	return bizpkg.AuditActor(ctx)
}
