package wechatinfra

import (
	"context"
	"log/slog"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	wechatrepo "glex/internal/domain/wechat/repository"
	"glex/internal/infra/ent"
	entapp "glex/internal/infra/ent/wechatapp"
	"glex/internal/infra/store"
	"glex/internal/infra/wechat/crypt"
	"glex/pkg/listq"

	"github.com/go-kratos/aip-go/ents"
)

// toBizApp maps an ent WechatApp PO to the domain object. The ciphertext
// stays out of the domain object — display reads the mask columns and
// never decrypts (ADR 0029). The callback address is derived by the
// domain service, not stored.
func toBizApp(po *ent.WechatApp) *entity.App {
	return &entity.App{
		ID:                 po.ID,
		Name:               po.Name,
		AppID:              po.WxAppID,
		AppType:            po.AppType,
		Kind:               po.AccountKind,
		Connection:         po.Connection,
		PlatformID:         po.PlatformID,
		Capabilities:       po.GrantedCapabilities,
		AppSecretMask:      po.AppSecretMask,
		TokenMask:          po.TokenMask,
		EncodingAESKeyMask: po.EncodingAesKeyMask,
		Suspended:          po.Suspended,
		Lapsed:             po.Lapsed,
		KeyVersion:         po.KeyVersion,
		CreateBy:           po.CreateBy,
		UpdateBy:           po.UpdateBy,
		CreatedAt:          po.CreatedAt,
		UpdatedAt:          po.UpdatedAt,
	}
}

type appRepo struct {
	data   *store.Data
	cipher *crypt.Cipher
	logger *slog.Logger
}

// NewAppRepo creates the WeChat App repo. The cipher owns the secret
// envelope at this boundary: writes seal the AppSecret and Push Secret,
// LoadSecrets opens them (ADR 0018).
func NewAppRepo(data *store.Data, cipher *crypt.Cipher) wechatrepo.AppRepo {
	return &appRepo{
		data:   data,
		cipher: cipher,
		logger: data.Logger.With(slog.String("component", "infra.wechat.app")),
	}
}

func (r *appRepo) storeError(ctx context.Context, operation string, err error) error {
	return wechatStoreError(ctx, r.logger, operation, err)
}

func (r *appRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *appRepo) FindByID(ctx context.Context, id int64) (*entity.App, error) {
	po, err := r.data.DB.WechatApp(ctx).Query().
		Where(entapp.IDEQ(id), entapp.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, wechat.ErrAppNotFound
		}
		return nil, r.storeError(ctx, "wechat.app.find_by_id", err)
	}
	return toBizApp(po), nil
}

func (r *appRepo) List(ctx context.Context, q listq.Query) ([]*entity.App, error) {
	pos, err := r.data.DB.WechatApp(ctx).Query().
		Where(
			entapp.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "wechat.app.list", err)
	}
	apps := make([]*entity.App, 0, len(pos))
	for _, po := range pos {
		apps = append(apps, toBizApp(po))
	}
	return apps, nil
}

func (r *appRepo) FindLiveByAppID(ctx context.Context, appID string) (*entity.App, error) {
	po, err := r.data.DB.WechatApp(ctx).Query().
		Where(
			entapp.WxAppIDEQ(appID),
			entapp.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, wechat.ErrAppNotFound
		}
		return nil, r.storeError(ctx, "wechat.app.find_live_by_app_id", err)
	}
	return toBizApp(po), nil
}

func (r *appRepo) CountLiveByPlatform(ctx context.Context, platformID int64) (int, error) {
	n, err := r.data.DB.WechatApp(ctx).Query().
		Where(
			entapp.PlatformIDEQ(platformID),
			entapp.ConnectionEQ(entity.ConnectionPlatformAuthorization),
			entapp.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return 0, r.storeError(ctx, "wechat.app.count_live_by_platform", err)
	}
	return n, nil
}

func (r *appRepo) LoadSecrets(ctx context.Context, id int64) (wechatrepo.AppSecrets, error) {
	po, err := r.data.DB.WechatApp(ctx).Query().
		Where(entapp.IDEQ(id), entapp.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return wechatrepo.AppSecrets{}, wechat.ErrAppNotFound
		}
		return wechatrepo.AppSecrets{}, r.storeError(ctx, "wechat.app.load_secrets", err)
	}
	values, err := r.cipher.Decrypt(po.SecretCipher)
	if err != nil {
		if isMasterKeyMissing(err) {
			return wechatrepo.AppSecrets{}, wechat.ErrMasterKeyMissing
		}
		return wechatrepo.AppSecrets{}, wechat.ErrSecretCrypto
	}
	return wechatrepo.AppSecrets{
		AppSecret:              values["app_secret"],
		Token:                  values["token"],
		EncodingAESKey:         values["encoding_aes_key"],
		AuthorizerRefreshToken: values["authorizer_refresh_token"],
	}, nil
}

func (r *appRepo) seal(secrets wechatrepo.AppSecrets) (string, error) {
	token, err := r.cipher.Encrypt(map[string]string{
		"app_secret":               secrets.AppSecret,
		"token":                    secrets.Token,
		"encoding_aes_key":         secrets.EncodingAESKey,
		"authorizer_refresh_token": secrets.AuthorizerRefreshToken,
	})
	if err != nil {
		if isMasterKeyMissing(err) {
			return "", wechat.ErrMasterKeyMissing
		}
		return "", wechat.ErrSecretCrypto
	}
	return token, nil
}

func (r *appRepo) Create(ctx context.Context, req wechatrepo.SaveAppRequest) (*entity.App, error) {
	app := req.App
	token, err := r.seal(req.Secrets)
	if err != nil {
		return nil, err
	}
	create := r.data.DB.WechatApp(ctx).Create().
		SetID(app.ID).
		SetName(app.Name).
		SetWxAppID(app.AppID).
		SetAppType(app.AppType).
		SetAccountKind(app.Kind).
		SetConnection(app.Connection).
		SetPlatformID(app.PlatformID).
		SetSecretCipher(token).
		SetAppSecretMask(app.AppSecretMask).
		SetTokenMask(app.TokenMask).
		SetEncodingAesKeyMask(app.EncodingAESKeyMask).
		SetSuspended(app.Suspended).
		SetLapsed(app.Lapsed).
		SetKeyVersion(app.KeyVersion).
		SetCreateBy(app.CreateBy).
		SetUpdateBy(app.UpdateBy)
	if app.Capabilities != nil {
		create.SetGrantedCapabilities(app.Capabilities)
	}
	po, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, wechat.ErrAppDuplicate
		}
		return nil, r.storeError(ctx, "wechat.app.create", err)
	}
	return toBizApp(po), nil
}

func (r *appRepo) Update(ctx context.Context, req wechatrepo.SaveAppRequest) (*entity.App, error) {
	app := req.App
	upd := r.data.DB.WechatApp(ctx).Update().
		Where(entapp.IDEQ(app.ID), entapp.DeletedAtIsNil()).
		SetName(app.Name).
		SetSuspended(app.Suspended).
		SetLapsed(app.Lapsed).
		SetUpdateBy(req.ActorID)
	if app.Capabilities != nil {
		upd.SetGrantedCapabilities(app.Capabilities)
	}
	if req.ReplaceSecrets {
		token, err := r.seal(req.Secrets)
		if err != nil {
			return nil, err
		}
		upd.SetSecretCipher(token).
			SetAppSecretMask(app.AppSecretMask).
			SetTokenMask(app.TokenMask).
			SetEncodingAesKeyMask(app.EncodingAESKeyMask).
			SetKeyVersion(app.KeyVersion)
	}
	n, err := upd.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, wechat.ErrAppDuplicate
		}
		return nil, r.storeError(ctx, "wechat.app.update", err)
	}
	if n == 0 {
		return nil, wechat.ErrAppNotFound
	}
	return r.FindByID(ctx, app.ID)
}

func (r *appRepo) Delete(ctx context.Context, req wechatrepo.DeleteAppRequest) error {
	n, err := r.data.DB.WechatApp(ctx).Update().
		Where(entapp.IDEQ(req.ID), entapp.DeletedAtIsNil()).
		SetDeletedAt(deletedAt()).
		SetUpdateBy(req.ActorID).
		Save(ctx)
	if err != nil {
		return r.storeError(ctx, "wechat.app.delete", err)
	}
	if n == 0 {
		return wechat.ErrAppNotFound
	}
	return nil
}
