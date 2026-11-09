package wechat

import (
	"context"

	wechatv1 "glex/api/wechat/v1"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func convertTemplate(row *entity.AdoptedTemplate) *wechatv1.AdoptedTemplate {
	return &wechatv1.AdoptedTemplate{
		TemplateId:        row.ID,
		AppId:             row.AppID,
		LibraryTemplateId: row.LibraryTemplateID,
		WechatTemplateId:  row.WechatTemplateID,
		Title:             row.Title,
		Channel:           channelProto(row.Channel),
		CreateBy:          row.CreateBy,
		UpdateBy:          row.UpdateBy,
		CreatedAt:         timestamppb.New(row.CreatedAt),
		UpdatedAt:         timestamppb.New(row.UpdatedAt),
	}
}

func convertDispatch(row *entity.Dispatch) *wechatv1.WechatDispatch {
	return &wechatv1.WechatDispatch{
		DispatchId:        row.ID,
		AppId:             row.AppID,
		Openid:            row.OpenID,
		TemplateId:        row.TemplateID,
		LibraryTemplateId: row.LibraryTemplateID,
		Channel:           channelProto(row.Channel),
		Content:           row.Content,
		WebUrl:            row.WebURL,
		MiniProgramPage:   row.MiniProgramPage,
		MiniProgramAppId:  row.MiniProgramAppID,
		Accepted:          row.Accepted,
		Msgid:             row.MsgID,
		Delivered:         row.Delivered,
		CreateBy:          row.CreateBy,
		UpdateBy:          row.UpdateBy,
		CreatedAt:         timestamppb.New(row.CreatedAt),
		UpdatedAt:         timestamppb.New(row.UpdatedAt),
	}
}

func channelProto(v string) wechatv1.WechatCapability {
	switch v {
	case entity.CapabilityTemplateMessage:
		return wechatv1.WechatCapability_WECHAT_CAPABILITY_TEMPLATE_MESSAGE
	case entity.CapabilitySubscribeNotification:
		return wechatv1.WechatCapability_WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION
	case entity.CapabilitySubscribeMessage:
		return wechatv1.WechatCapability_WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE
	default:
		return wechatv1.WechatCapability_WECHAT_CAPABILITY_UNSPECIFIED
	}
}

func channelDomain(v wechatv1.WechatCapability) string {
	switch v {
	case wechatv1.WechatCapability_WECHAT_CAPABILITY_TEMPLATE_MESSAGE:
		return entity.CapabilityTemplateMessage
	case wechatv1.WechatCapability_WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION:
		return entity.CapabilitySubscribeNotification
	case wechatv1.WechatCapability_WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE:
		return entity.CapabilitySubscribeMessage
	default:
		return ""
	}
}

// dispatchListSpec pages the dispatch list. app_id is a request field,
// not a secret, and selects attempts whose app may already be withdrawn.
var dispatchListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("app_id", filtering.TypeInt),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListAdoptedTemplates returns templates adopted onto this app.
func (s *WechatApplication) ListAdoptedTemplates(ctx context.Context, req *wechatv1.ListAdoptedTemplatesRequest) (*wechatv1.AdoptedTemplateSet, error) {
	rows, err := s.apps.ListTemplates(ctx, req.GetAppId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_templates", err)
		return nil, err
	}
	set := &wechatv1.AdoptedTemplateSet{Templates: make([]*wechatv1.AdoptedTemplate, 0, len(rows))}
	for _, row := range rows {
		set.Templates = append(set.Templates, convertTemplate(row))
	}
	return set, nil
}

// AdoptWechatTemplate adopts one public-library template. create_by is
// the authenticated Principal. The request does not carry audit fields.
func (s *WechatApplication) AdoptWechatTemplate(ctx context.Context, req *wechatv1.AdoptWechatTemplateRequest) (*wechatv1.AdoptedTemplate, error) {
	var created *entity.AdoptedTemplate
	err := s.inTx(ctx, "wechat.adopt_template", func(ctx context.Context) error {
		var callErr error
		created, callErr = s.apps.AdoptTemplate(ctx, wechatservice.AdoptTemplateRequest{
			AppID:             req.GetAppId(),
			LibraryTemplateID: req.GetLibraryTemplateId(),
			Channel:           channelDomain(req.GetChannel()),
			ActorID:           s.actor(ctx),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	return convertTemplate(created), nil
}

// ListWechatDispatches lists send attempts. A withdrawn app's attempts
// stay in the list. app_id narrows the page to one app.
func (s *WechatApplication) ListWechatDispatches(ctx context.Context, req *wechatv1.ListWechatDispatchesRequest) (*wechatv1.WechatDispatchSet, error) {
	q, page, err := listq.Parse(req, dispatchListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_dispatches", err)
		return nil, err
	}
	got, err := s.apps.ListDispatches(ctx, wechatservice.ListDispatchesRequest{
		AppID:  req.GetAppId(),
		Limit:  q.Limit,
		Offset: q.Offset,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_dispatches", err)
		return nil, err
	}
	set := &wechatv1.WechatDispatchSet{Dispatches: make([]*wechatv1.WechatDispatch, 0, len(got.Dispatches))}
	for _, row := range got.Dispatches {
		set.Dispatches = append(set.Dispatches, convertDispatch(row))
	}
	set.NextPageToken = page.NextToken(len(got.Dispatches))
	return set, nil
}

// CreateWechatDispatch sends one template to one openid. The actor is
// the authenticated Principal. A business rejection is returned as that
// error after the rejected dispatch has been recorded.
func (s *WechatApplication) CreateWechatDispatch(ctx context.Context, req *wechatv1.CreateWechatDispatchRequest) (*wechatv1.WechatDispatch, error) {
	var created *entity.Dispatch
	err := s.inTx(ctx, "wechat.create_dispatch", func(ctx context.Context) error {
		var callErr error
		created, callErr = s.apps.Send(ctx, wechatservice.SendDispatchRequest{
			AppID:            req.GetAppId(),
			TemplateID:       req.GetTemplateId(),
			OpenID:           req.GetOpenid(),
			Content:          req.GetContent(),
			WebURL:           req.GetWebUrl(),
			MiniProgramAppID: req.GetMiniProgramAppId(),
			MiniProgramPage:  req.GetMiniProgramPage(),
			ActorID:          s.actor(ctx),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	return convertDispatch(created), nil
}
