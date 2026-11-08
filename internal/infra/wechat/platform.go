package wechatinfra

import (
	"context"
	"log/slog"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	wechatrepo "glex/internal/domain/wechat/repository"
	"glex/internal/infra/ent"
	entplatform "glex/internal/infra/ent/wechatplatform"
	"glex/internal/infra/store"
	"glex/internal/infra/wechat/crypt"
	"glex/pkg/listq"

	"github.com/go-kratos/aip-go/ents"
)

// toBizPlatform maps an ent WechatPlatform PO to the domain object. The
// ciphertext stays out of the domain object — display reads the mask
// columns and never decrypts (ADR 0029).
func toBizPlatform(po *ent.WechatPlatform) *entity.Platform {
	return &entity.Platform{
		ID:                  po.ID,
		Name:                po.Name,
		ComponentAppID:      po.ComponentAppID,
		ComponentSecretMask: po.ComponentSecretMask,
		TokenMask:           po.TokenMask,
		EncodingAESKeyMask:  po.EncodingAesKeyMask,
		KeyVersion:          po.KeyVersion,
		CreateBy:            po.CreateBy,
		UpdateBy:            po.UpdateBy,
		CreatedAt:           po.CreatedAt,
		UpdatedAt:           po.UpdatedAt,
	}
}

type platformRepo struct {
	data   *store.Data
	cipher *crypt.Cipher
	logger *slog.Logger
}

// NewPlatformRepo creates the WeChat Platform repo. The cipher owns the
// secret envelope at this boundary: writes seal the secret triple,
// LoadSecrets opens it (ADR 0018).
func NewPlatformRepo(data *store.Data, cipher *crypt.Cipher) wechatrepo.PlatformRepo {
	return &platformRepo{
		data:   data,
		cipher: cipher,
		logger: data.Logger.With(slog.String("component", "infra.wechat.platform")),
	}
}

func (r *platformRepo) storeError(ctx context.Context, operation string, err error) error {
	return wechatStoreError(ctx, r.logger, operation, err)
}

func (r *platformRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *platformRepo) FindByID(ctx context.Context, id int64) (*entity.Platform, error) {
	po, err := r.data.DB.WechatPlatform(ctx).Query().
		Where(entplatform.IDEQ(id), entplatform.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, wechat.ErrPlatformNotFound
		}
		return nil, r.storeError(ctx, "wechat.platform.find_by_id", err)
	}
	return toBizPlatform(po), nil
}

func (r *platformRepo) List(ctx context.Context, q listq.Query) ([]*entity.Platform, error) {
	pos, err := r.data.DB.WechatPlatform(ctx).Query().
		Where(
			entplatform.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "wechat.platform.list", err)
	}
	platforms := make([]*entity.Platform, 0, len(pos))
	for _, po := range pos {
		platforms = append(platforms, toBizPlatform(po))
	}
	return platforms, nil
}

func (r *platformRepo) FindLiveByComponentAppID(ctx context.Context, componentAppID string) (*entity.Platform, error) {
	po, err := r.data.DB.WechatPlatform(ctx).Query().
		Where(
			entplatform.ComponentAppIDEQ(componentAppID),
			entplatform.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, wechat.ErrPlatformNotFound
		}
		return nil, r.storeError(ctx, "wechat.platform.find_live_by_component_app_id", err)
	}
	return toBizPlatform(po), nil
}

func (r *platformRepo) LoadSecrets(ctx context.Context, id int64) (wechatrepo.Secrets, error) {
	po, err := r.data.DB.WechatPlatform(ctx).Query().
		Where(entplatform.IDEQ(id), entplatform.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return wechatrepo.Secrets{}, wechat.ErrPlatformNotFound
		}
		return wechatrepo.Secrets{}, r.storeError(ctx, "wechat.platform.load_secrets", err)
	}
	values, err := r.cipher.Decrypt(po.SecretCipher)
	if err != nil {
		if isMasterKeyMissing(err) {
			return wechatrepo.Secrets{}, wechat.ErrMasterKeyMissing
		}
		return wechatrepo.Secrets{}, wechat.ErrSecretCrypto
	}
	return wechatrepo.Secrets{
		ComponentSecret: values["component_secret"],
		Token:           values["token"],
		EncodingAESKey:  values["encoding_aes_key"],
	}, nil
}

func (r *platformRepo) seal(secrets wechatrepo.Secrets) (string, error) {
	token, err := r.cipher.Encrypt(map[string]string{
		"component_secret": secrets.ComponentSecret,
		"token":            secrets.Token,
		"encoding_aes_key": secrets.EncodingAESKey,
	})
	if err != nil {
		if isMasterKeyMissing(err) {
			return "", wechat.ErrMasterKeyMissing
		}
		return "", wechat.ErrSecretCrypto
	}
	return token, nil
}

func (r *platformRepo) Create(ctx context.Context, req wechatrepo.SavePlatformRequest) (*entity.Platform, error) {
	p := req.Platform
	token, err := r.seal(req.Secrets)
	if err != nil {
		return nil, err
	}
	po, err := r.data.DB.WechatPlatform(ctx).Create().
		SetID(p.ID).
		SetName(p.Name).
		SetComponentAppID(p.ComponentAppID).
		SetSecretCipher(token).
		SetComponentSecretMask(p.ComponentSecretMask).
		SetTokenMask(p.TokenMask).
		SetEncodingAesKeyMask(p.EncodingAESKeyMask).
		SetKeyVersion(p.KeyVersion).
		SetCreateBy(p.CreateBy).
		SetUpdateBy(p.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, wechat.ErrPlatformDuplicate
		}
		return nil, r.storeError(ctx, "wechat.platform.create", err)
	}
	return toBizPlatform(po), nil
}

func (r *platformRepo) Update(ctx context.Context, req wechatrepo.SavePlatformRequest) (*entity.Platform, error) {
	p := req.Platform
	upd := r.data.DB.WechatPlatform(ctx).Update().
		Where(entplatform.IDEQ(p.ID), entplatform.DeletedAtIsNil()).
		SetName(p.Name).
		SetUpdateBy(req.ActorID)
	if req.ReplaceSecrets {
		token, err := r.seal(req.Secrets)
		if err != nil {
			return nil, err
		}
		upd.SetSecretCipher(token).
			SetComponentSecretMask(p.ComponentSecretMask).
			SetTokenMask(p.TokenMask).
			SetEncodingAesKeyMask(p.EncodingAESKeyMask).
			SetKeyVersion(p.KeyVersion)
	}
	n, err := upd.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, wechat.ErrPlatformDuplicate
		}
		return nil, r.storeError(ctx, "wechat.platform.update", err)
	}
	if n == 0 {
		return nil, wechat.ErrPlatformNotFound
	}
	return r.FindByID(ctx, p.ID)
}

func (r *platformRepo) Delete(ctx context.Context, req wechatrepo.DeletePlatformRequest) error {
	n, err := r.data.DB.WechatPlatform(ctx).Update().
		Where(entplatform.IDEQ(req.ID), entplatform.DeletedAtIsNil()).
		SetDeletedAt(deletedAt()).
		SetUpdateBy(req.ActorID).
		Save(ctx)
	if err != nil {
		return r.storeError(ctx, "wechat.platform.delete", err)
	}
	if n == 0 {
		return wechat.ErrPlatformNotFound
	}
	return nil
}
