package payinfra

import (
	"context"
	"log/slog"

	"glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/ent"
	entmerchant "glex/internal/infra/ent/paymerchantaccount"
	"glex/internal/infra/pay/crypt"
	"glex/internal/infra/store"

	"github.com/go-kratos/aip-go/ents"
)

// toBizMerchant maps an ent PayMerchantAccount PO to a payentity
// .MerchantAccount DO. Config stays nil — the ciphertext is only opened by
// LoadConfig, never on display paths (ADR 0018).
func toBizMerchant(po *ent.PayMerchantAccount) *payentity.MerchantAccount {
	return &payentity.MerchantAccount{
		ID:           po.ID,
		Channel:      payentity.Channel(po.Channel),
		Name:         po.Name,
		Status:       payentity.MerchantStatus(po.Status),
		ConfigCipher: po.ConfigCipher,
		ConfigMasked: po.ConfigMasked,
		ConfigDigest: po.ConfigDigest,
		KeyVersion:   po.KeyVersion,
		Remark:       po.Remark,
		CreateBy:     po.CreateBy,
		UpdateBy:     po.UpdateBy,
		CreatedAt:    po.CreatedAt,
		UpdatedAt:    po.UpdatedAt,
	}
}

type merchantAccountRepo struct {
	data   *store.Data
	cipher *crypt.Cipher
	logger *slog.Logger
}

// NewMerchantAccountRepo creates the Merchant Account repo. The cipher
// owns the config envelope at this boundary: writes seal Config whole,
// LoadConfig opens it (ADR 0018).
func NewMerchantAccountRepo(data *store.Data, cipher *crypt.Cipher) payrepo.MerchantAccountRepo {
	return &merchantAccountRepo{
		data:   data,
		cipher: cipher,
		logger: data.Logger.With(slog.String("component", "infra.pay.merchant")),
	}
}

// storeError is this repo's log-and-map (ADR 0007).
func (r *merchantAccountRepo) storeError(ctx context.Context, operation string, err error) error {
	return payStoreError(ctx, r.logger, operation, err)
}

func (r *merchantAccountRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *merchantAccountRepo) FindByID(ctx context.Context, id int64) (*payentity.MerchantAccount, error) {
	po, err := r.data.DB.PayMerchantAccount(ctx).Query().
		Where(entmerchant.IDEQ(id), entmerchant.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrMerchantNotFound
		}
		return nil, r.storeError(ctx, "pay.merchant.find_by_id", err)
	}
	return toBizMerchant(po), nil
}

// FindByIDAny resolves one Merchant Account regardless of withdrawal —
// the settlement paths (prepay, query, close, notify) must keep resolving
// a withdrawn merchant's snapshot channel, while display and editing
// paths stay strict (design §7: withdrawal never blocks in-flight
// payments).
func (r *merchantAccountRepo) FindByIDAny(ctx context.Context, id int64) (*payentity.MerchantAccount, error) {
	po, err := r.data.DB.PayMerchantAccount(ctx).Query().
		Where(entmerchant.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrMerchantNotFound
		}
		return nil, r.storeError(ctx, "pay.merchant.find_by_id_any", err)
	}
	return toBizMerchant(po), nil
}

func (r *merchantAccountRepo) List(ctx context.Context, opts ...payrepo.ListOption) ([]*payentity.MerchantAccount, error) {
	o := payrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.PayMerchantAccount(ctx).Query().
		Where(
			entmerchant.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.merchant.list", err)
	}
	merchants := make([]*payentity.MerchantAccount, 0, len(pos))
	for _, po := range pos {
		merchants = append(merchants, toBizMerchant(po))
	}
	return merchants, nil
}

// LoadConfig decrypts the stored config into Config. The master-key
// sentinel passes through untouched; every other failure maps to the
// module's crypto error (the handling site, ADR 0007).
func (r *merchantAccountRepo) LoadConfig(ctx context.Context, m *payentity.MerchantAccount) error {
	values, err := r.cipher.Decrypt(m.ConfigCipher)
	if err != nil {
		if isMasterKeyMissing(err) {
			return pay.ErrMasterKeyMissing
		}
		return pay.ErrConfigCrypto
	}
	m.Config = values
	return nil
}

// sealConfig encrypts a value set, mapping the master-key sentinel to the
// module's reason-carrying error and every other failure to the crypto
// error (the handling site, ADR 0007).
func (r *merchantAccountRepo) sealConfig(values map[string]string) (string, error) {
	token, err := r.cipher.Encrypt(values)
	if err != nil {
		if isMasterKeyMissing(err) {
			return "", pay.ErrMasterKeyMissing
		}
		return "", pay.ErrConfigCrypto
	}
	return token, nil
}

func (r *merchantAccountRepo) Create(ctx context.Context, m *payentity.MerchantAccount) (*payentity.MerchantAccount, error) {
	token, err := r.sealConfig(m.Config)
	if err != nil {
		return nil, err
	}
	po, err := r.data.DB.PayMerchantAccount(ctx).Create().
		SetID(m.ID).
		SetChannel(int16(m.Channel)).
		SetName(m.Name).
		SetStatus(int16(m.Status)).
		SetConfigCipher(token).
		SetConfigMasked(m.ConfigMasked).
		SetConfigDigest(m.ConfigDigest).
		SetKeyVersion(m.KeyVersion).
		SetRemark(m.Remark).
		SetCreateBy(m.CreateBy).
		SetUpdateBy(m.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, pay.ErrMerchantDuplicate
		}
		return nil, r.storeError(ctx, "pay.merchant.create", err)
	}
	return toBizMerchant(po), nil
}

func (r *merchantAccountRepo) Update(ctx context.Context, m *payentity.MerchantAccount, actorID int64) (*payentity.MerchantAccount, error) {
	token, err := r.sealConfig(m.Config)
	if err != nil {
		return nil, err
	}
	po, err := r.data.DB.PayMerchantAccount(ctx).UpdateOneID(m.ID).
		SetName(m.Name).
		SetConfigCipher(token).
		SetConfigMasked(m.ConfigMasked).
		SetConfigDigest(m.ConfigDigest).
		SetRemark(m.Remark).
		SetUpdateBy(actorID).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrMerchantNotFound
		}
		if ent.IsConstraintError(err) {
			return nil, pay.ErrMerchantDuplicate
		}
		return nil, r.storeError(ctx, "pay.merchant.update", err)
	}
	return toBizMerchant(po), nil
}

func (r *merchantAccountRepo) UpdateStatus(ctx context.Context, id int64, status payentity.MerchantStatus, actorID int64) error {
	err := r.data.DB.PayMerchantAccount(ctx).UpdateOneID(id).
		SetStatus(int16(status)).
		SetUpdateBy(actorID).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return pay.ErrMerchantNotFound
	}
	if err != nil {
		return r.storeError(ctx, "pay.merchant.update_status", err)
	}
	return nil
}

func (r *merchantAccountRepo) Delete(ctx context.Context, id, actorID int64) error {
	n, err := r.data.DB.PayMerchantAccount(ctx).Update().
		Where(entmerchant.IDEQ(id), entmerchant.DeletedAtIsNil()).
		SetDeletedAt(deletedAt()).
		SetUpdateBy(actorID).
		Save(ctx)
	if err != nil {
		return r.storeError(ctx, "pay.merchant.delete", err)
	}
	if n == 0 {
		return pay.ErrMerchantNotFound
	}
	return nil
}
