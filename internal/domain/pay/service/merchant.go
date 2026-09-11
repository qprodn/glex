package service

import (
	"context"
	"log/slog"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// CreateInput is what a merchant-create use case states.
type CreateInput struct {
	Channel payentity.Channel
	Name    string
	Config  map[string]string
	Remark  string
}

// UpdateInput is what a merchant-update use case states. Secret config
// values left blank keep their stored values (schema semantics, ADR 0021).
type UpdateInput struct {
	ID      int64
	Name    string
	Config  map[string]string
	Remark  string
}

// ProbeOutcome is one Connectivity Test's result: ok plus a human-readable
// message and the round-trip latency. The test moves no money.
type ProbeOutcome struct {
	OK        bool
	Message   string
	LatencyMS int64
}

// Merchant is the merchant domain service: the only path to Merchant
// Account rows and the seam where the config envelope is applied — schema
// validation, whole-config encryption (via the repo boundary), the masked
// echo, and the digest the gateway cache keys on (ADR 0018, ADR 0021).
type Merchant interface {
	Get(ctx context.Context, id int64) (*payentity.MerchantAccount, error)
	List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*payentity.MerchantAccount, error)
	// Create validates the config against the channel schema, seals it,
	// and records the account (enabled).
	Create(ctx context.Context, in *CreateInput, actorID int64) (*payentity.MerchantAccount, error)
	// Update replaces the display fields and the whole config form, with
	// blank secret fields keeping their stored values.
	Update(ctx context.Context, in *UpdateInput, actorID int64) (*payentity.MerchantAccount, error)
	// ChangeStatus flips the enable switch; disabling takes the merchant's
	// cached gateways out of service.
	ChangeStatus(ctx context.Context, id int64, status payentity.MerchantStatus, actorID int64) (*payentity.MerchantAccount, error)
	// Withdraw soft-deletes the account and drops its cached gateways.
	Withdraw(ctx context.Context, id, actorID int64) error
	// Probe runs the Connectivity Test through the merchant's gateway.
	Probe(ctx context.Context, id int64) (*ProbeOutcome, error)
	// Schemas lists every adopted channel's config schema.
	Schemas() []payentity.ChannelSchema
	// Adopted reports whether the channel can collect.
	Adopted(channel payentity.Channel) bool
	// ResolveGateway decrypts the merchant's config and resolves its
	// gateway — the shared intake for channel-touching paths outside
	// merchant CRUD (notification intake today). The config decryption
	// failure surfaces as the module's master-key/crypto sentinels.
	ResolveGateway(ctx context.Context, merchantID int64) (*payentity.MerchantAccount, ChannelGateway, error)
}

type merchantImpl struct {
	merchants payrepo.MerchantAccountRepo
	catalog   ChannelCatalog
	cipher    ConfigCipher
	gateways  GatewayRegistry
	logger    *slog.Logger
}

// NewMerchantService creates the merchant domain service.
func NewMerchantService(
	merchants payrepo.MerchantAccountRepo,
	catalog ChannelCatalog,
	cipher ConfigCipher,
	gateways GatewayRegistry,
	logger *slog.Logger,
) Merchant {
	return &merchantImpl{
		merchants: merchants,
		catalog:   catalog,
		cipher:    cipher,
		gateways:  gateways,
		logger:    logger.With(slog.String("component", "domain.pay.merchant.svc")),
	}
}

func (s *merchantImpl) Get(ctx context.Context, id int64) (*payentity.MerchantAccount, error) {
	m, err := s.merchants.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.get", err)
		return nil, err
	}
	return m, nil
}

func (s *merchantImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*payentity.MerchantAccount, error) {
	ms, err := s.merchants.List(ctx,
		payrepo.ListFilter(filter),
		payrepo.ListOrderBy(orderBy),
		payrepo.ListLimit(limit),
		payrepo.ListOffset(offset),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.list", err)
		return nil, err
	}
	return ms, nil
}

func (s *merchantImpl) Create(ctx context.Context, in *CreateInput, actorID int64) (*payentity.MerchantAccount, error) {
	if err := validateDisplayName(in.Name); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.create", err)
		return nil, err
	}
	if len(in.Remark) > 500 {
		err := pay.ErrMerchantInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.create", err)
		return nil, err
	}
	if !s.catalog.Adopted(in.Channel) {
		err := pay.ErrChannelNotAdopted
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.create", err)
		return nil, err
	}
	cleaned, err := s.catalog.ValidateConfig(in.Channel, in.Config)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.create", err)
		return nil, err
	}
	m := &payentity.MerchantAccount{
		ID:           s.merchants.NextID(),
		Channel:      in.Channel,
		Name:         in.Name,
		Status:       payentity.MerchantEnabled,
		Config:       cleaned,
		ConfigMasked: s.catalog.MaskConfig(in.Channel, cleaned),
		ConfigDigest: s.cipher.Digest(cleaned),
		KeyVersion:   1,
		Remark:       in.Remark,
		CreateBy:     actorID,
		UpdateBy:     actorID,
	}
	created, err := s.merchants.Create(ctx, m)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.create", err)
		return nil, err
	}
	return created, nil
}

func (s *merchantImpl) Update(ctx context.Context, in *UpdateInput, actorID int64) (*payentity.MerchantAccount, error) {
	if err := validateDisplayName(in.Name); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	if len(in.Remark) > 500 {
		err := pay.ErrMerchantInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	m, err := s.merchants.FindByID(ctx, in.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	if !s.catalog.Adopted(m.Channel) {
		err = pay.ErrChannelNotAdopted
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	// Blank secret fields keep their stored values — the one place the
	// display round-trip touches plaintext (ADR 0021's schema semantics).
	if err := s.merchants.LoadConfig(ctx, m); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	schema, ok := s.catalog.Schema(m.Channel)
	if !ok {
		err = pay.ErrChannelNotAdopted
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	merged := mergeBlankSecrets(schema, m.Config, in.Config)
	cleaned, err := s.catalog.ValidateConfig(m.Channel, merged)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	m.Name = in.Name
	m.Remark = in.Remark
	m.Config = cleaned
	m.ConfigMasked = s.catalog.MaskConfig(m.Channel, cleaned)
	m.ConfigDigest = s.cipher.Digest(cleaned)
	updated, err := s.merchants.Update(ctx, m, actorID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.update", err)
		return nil, err
	}
	// The old config's gateways are dead the moment the new one is stored.
	s.gateways.Invalidate(in.ID)
	return updated, nil
}

func (s *merchantImpl) ChangeStatus(ctx context.Context, id int64, status payentity.MerchantStatus, actorID int64) (*payentity.MerchantAccount, error) {
	if status != payentity.MerchantEnabled && status != payentity.MerchantDisabled {
		err := pay.ErrMerchantInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.change_status", err)
		return nil, err
	}
	if err := s.merchants.UpdateStatus(ctx, id, status, actorID); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.change_status", err)
		return nil, err
	}
	// Disabled or re-enabled, the cached gateways start from scratch.
	s.gateways.Invalidate(id)
	return s.Get(ctx, id)
}

func (s *merchantImpl) Withdraw(ctx context.Context, id, actorID int64) error {
	if err := s.merchants.Delete(ctx, id, actorID); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.withdraw", err)
		return err
	}
	s.gateways.Invalidate(id)
	return nil
}

// Probe runs the Connectivity Test: resolve the merchant's gateway (which
// forces the config decryption — the thing being tested) and make the
// channel's cheap authenticated call. A gateway failure is an outcome
// (ok=false), not a transport error; an unadopted channel or a missing
// master key is a business error.
func (s *merchantImpl) Probe(ctx context.Context, id int64) (*ProbeOutcome, error) {
	m, err := s.merchants.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.probe", err)
		return nil, err
	}
	if err := s.merchants.LoadConfig(ctx, m); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.probe", err)
		return nil, err
	}
	gw, err := s.gateways.Resolve(ctx, m)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.probe", err)
		return nil, err
	}
	start := time.Now()
	probeErr := gw.Probe(ctx, m)
	latency := time.Since(start).Milliseconds()
	outcome := &ProbeOutcome{LatencyMS: latency}
	if probeErr != nil {
		outcome.OK = false
		outcome.Message = truncateMessage(probeErr.Error(), 256)
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.probe", probeErr)
		return outcome, nil
	}
	outcome.OK = true
	outcome.Message = "连接成功"
	return outcome, nil
}

func (s *merchantImpl) ResolveGateway(ctx context.Context, merchantID int64) (*payentity.MerchantAccount, ChannelGateway, error) {
	// Settlement paths resolve past withdrawal: a withdrawn merchant's
	// in-flight payments still settle on its snapshot channel (design §7),
	// while display and editing paths keep the strict lookup. The shared
	// seam logs its own breadcrumbs; the wrapping op names this site.
	m, gw, err := resolveMerchantGateway(ctx, s.merchants, s.gateways, merchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.merchant.resolve_gateway", err)
		return nil, nil, err
	}
	return m, gw, nil
}

func (s *merchantImpl) Schemas() []payentity.ChannelSchema {
	return s.catalog.Schemas()
}

func (s *merchantImpl) Adopted(channel payentity.Channel) bool {
	return s.catalog.Adopted(channel)
}

// validateDisplayName enforces the display-name invariants shared by
// create and update; uniqueness is the partial unique index's job and
// surfaces as ErrMerchantDuplicate from the repo.
func validateDisplayName(name string) error {
	if name == "" || len(name) > 64 {
		return pay.ErrMerchantInvalid
	}
	return nil
}

// mergeBlankSecrets substitutes the stored value for every secret field
// the caller left blank — a missing key is as blank as an empty string
// (design §7: 敏感字段留空 = 保持原值); plain fields pass through verbatim,
// where a missing key means blank.
func mergeBlankSecrets(schema payentity.ChannelSchema, stored, submitted map[string]string) map[string]string {
	merged := make(map[string]string, len(schema.Fields))
	for _, field := range schema.Fields {
		v, ok := submitted[field.Name]
		if field.Secret && (!ok || v == "") {
			merged[field.Name] = stored[field.Name]
			continue
		}
		merged[field.Name] = v
	}
	return merged
}

// truncateMessage bounds channel error text before it reaches an API
// response.
func truncateMessage(msg string, max int) string {
	if len(msg) <= max {
		return msg
	}
	return msg[:max]
}
