package wechatinfra

import (
	"context"
	"log/slog"

	"entgo.io/ent/dialect/sql"

	"glex/internal/domain/wechat/entity"
	wechatrepo "glex/internal/domain/wechat/repository"
	"glex/internal/infra/ent"
	entdispatch "glex/internal/infra/ent/wechatdispatch"
	"glex/internal/infra/store"
)

func toBizDispatch(po *ent.WechatDispatch) *entity.Dispatch {
	return &entity.Dispatch{
		ID:                po.ID,
		AppID:             po.AppID,
		OpenID:            po.Openid,
		TemplateID:        po.TemplateID,
		LibraryTemplateID: po.LibraryTemplateID,
		WechatTemplateID:  po.WechatTemplateID,
		Channel:           po.Channel,
		Content:           po.Content,
		WebURL:            po.WebURL,
		MiniProgramPage:   po.MiniProgramPage,
		Accepted:          po.Accepted,
		MsgID:             po.Msgid,
		CreateBy:          po.CreateBy,
		UpdateBy:          po.UpdateBy,
		CreatedAt:         po.CreatedAt,
		UpdatedAt:         po.UpdatedAt,
	}
}

type dispatchRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewDispatchRepo creates the WeChat Dispatch repo. Rows are not
// hard-deleted. Withdrawing the app does not withdraw them, so the list
// does not join the app's deleted_at.
func NewDispatchRepo(data *store.Data) wechatrepo.DispatchRepo {
	return &dispatchRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.wechat.dispatch")),
	}
}

func (r *dispatchRepo) NextID() int64 { return newSnowflakeID() }

func (r *dispatchRepo) Create(ctx context.Context, row *entity.Dispatch) (*entity.Dispatch, error) {
	po, err := r.data.DB.WechatDispatch(ctx).Create().
		SetID(row.ID).
		SetAppID(row.AppID).
		SetOpenid(row.OpenID).
		SetTemplateID(row.TemplateID).
		SetLibraryTemplateID(row.LibraryTemplateID).
		SetWechatTemplateID(row.WechatTemplateID).
		SetChannel(row.Channel).
		SetContent(row.Content).
		SetWebURL(row.WebURL).
		SetMiniProgramPage(row.MiniProgramPage).
		SetAccepted(row.Accepted).
		SetMsgid(row.MsgID).
		SetCreateBy(row.CreateBy).
		SetUpdateBy(row.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, wechatStoreError(ctx, r.logger, "wechat.dispatch.create", err)
	}
	return toBizDispatch(po), nil
}

func (r *dispatchRepo) List(ctx context.Context, q wechatrepo.DispatchQuery) ([]*entity.Dispatch, error) {
	query := r.data.DB.WechatDispatch(ctx).Query().
		Where(entdispatch.DeletedAtIsNil()).
		Order(entdispatch.ByID(sql.OrderDesc()))
	if q.AppID != 0 {
		query = query.Where(entdispatch.AppIDEQ(q.AppID))
	}
	if q.Offset > 0 {
		query = query.Offset(q.Offset)
	}
	if q.Limit > 0 {
		query = query.Limit(q.Limit)
	}
	pos, err := query.All(ctx)
	if err != nil {
		return nil, wechatStoreError(ctx, r.logger, "wechat.dispatch.list", err)
	}
	out := make([]*entity.Dispatch, 0, len(pos))
	for _, po := range pos {
		out = append(out, toBizDispatch(po))
	}
	return out, nil
}
