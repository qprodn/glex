package wechatinfra

import (
	"context"
	"log/slog"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	wechatrepo "glex/internal/domain/wechat/repository"
	"glex/internal/infra/ent"
	enttemplate "glex/internal/infra/ent/wechatadoptedtemplate"
	"glex/internal/infra/store"
)

func toBizTemplate(po *ent.WechatAdoptedTemplate) *entity.AdoptedTemplate {
	return &entity.AdoptedTemplate{
		ID:                po.ID,
		AppID:             po.AppID,
		LibraryTemplateID: po.LibraryTemplateID,
		WechatTemplateID:  po.WechatTemplateID,
		Title:             po.Title,
		Channel:           po.Channel,
		CreateBy:          po.CreateBy,
		UpdateBy:          po.UpdateBy,
		CreatedAt:         po.CreatedAt,
		UpdatedAt:         po.UpdatedAt,
	}
}

type templateRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewTemplateRepo creates the Adopted Template repo. Rows are not
// hard-deleted. Withdrawing the app does not withdraw them.
func NewTemplateRepo(data *store.Data) wechatrepo.TemplateRepo {
	return &templateRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.wechat.template")),
	}
}

func (r *templateRepo) NextID() int64 { return newSnowflakeID() }

func (r *templateRepo) FindByID(ctx context.Context, id int64) (*entity.AdoptedTemplate, error) {
	po, err := r.data.DB.WechatAdoptedTemplate(ctx).Query().
		Where(enttemplate.IDEQ(id), enttemplate.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, wechat.ErrTemplateNotFound
		}
		return nil, wechatStoreError(ctx, r.logger, "wechat.template.find_by_id", err)
	}
	return toBizTemplate(po), nil
}

func (r *templateRepo) FindByAppAndLibrary(ctx context.Context, req wechatrepo.FindByAppAndLibraryRequest) (*entity.AdoptedTemplate, error) {
	po, err := r.data.DB.WechatAdoptedTemplate(ctx).Query().
		Where(
			enttemplate.AppIDEQ(req.AppID),
			enttemplate.LibraryTemplateIDEQ(req.LibraryTemplateID),
			enttemplate.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, wechat.ErrTemplateNotFound
		}
		return nil, wechatStoreError(ctx, r.logger, "wechat.template.find_by_app_and_library", err)
	}
	return toBizTemplate(po), nil
}

func (r *templateRepo) ListByApp(ctx context.Context, appID int64) ([]*entity.AdoptedTemplate, error) {
	pos, err := r.data.DB.WechatAdoptedTemplate(ctx).Query().
		Where(enttemplate.AppIDEQ(appID), enttemplate.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, wechatStoreError(ctx, r.logger, "wechat.template.list_by_app", err)
	}
	out := make([]*entity.AdoptedTemplate, 0, len(pos))
	for _, po := range pos {
		out = append(out, toBizTemplate(po))
	}
	return out, nil
}

func (r *templateRepo) Create(ctx context.Context, row *entity.AdoptedTemplate) (*entity.AdoptedTemplate, error) {
	po, err := r.data.DB.WechatAdoptedTemplate(ctx).Create().
		SetID(row.ID).
		SetAppID(row.AppID).
		SetLibraryTemplateID(row.LibraryTemplateID).
		SetWechatTemplateID(row.WechatTemplateID).
		SetTitle(row.Title).
		SetChannel(row.Channel).
		SetCreateBy(row.CreateBy).
		SetUpdateBy(row.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, wechat.ErrTemplateDuplicate
		}
		return nil, wechatStoreError(ctx, r.logger, "wechat.template.create", err)
	}
	return toBizTemplate(po), nil
}
