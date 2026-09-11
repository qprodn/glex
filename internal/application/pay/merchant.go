package pay

import (
	"context"

	payv1 "glex/api/pay/v1"
	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertMerchant maps a payentity.MerchantAccount DO to the proto DTO.
// Only the masked echo crosses the wire — the plaintext config and the
// ciphertext both stay server-side (ADR 0018).
func convertMerchant(m *payentity.MerchantAccount) *payv1.MerchantAccount {
	masked := make(map[string]string, len(m.ConfigMasked))
	for k, v := range m.ConfigMasked {
		masked[k] = v
	}
	return &payv1.MerchantAccount{
		MerchantId:   m.ID,
		Channel:      int32(m.Channel),
		Name:         m.Name,
		Status:       int32(m.Status),
		ConfigMasked: masked,
		ConfigDigest: m.ConfigDigest,
		KeyVersion:   int32(m.KeyVersion),
		Remark:       m.Remark,
		CreateBy:     m.CreateBy,
		UpdateBy:     m.UpdateBy,
		CreatedAt:    timestamppb.New(m.CreatedAt),
		UpdatedAt:    timestamppb.New(m.UpdatedAt),
	}
}

// convertSchema maps a channel config schema to its proto shape; the
// adopted flag is composed by the caller (it is registry state, not schema
// data).
func convertSchema(schema payentity.ChannelSchema, adopted bool) *payv1.ChannelSchema {
	out := &payv1.ChannelSchema{
		Channel: int32(schema.Channel),
		Name:    schema.Name,
		Label:   schema.Label,
		Adopted: adopted,
		Fields:  make([]*payv1.SchemaField, 0, len(schema.Fields)),
	}
	for _, f := range schema.Fields {
		pf := &payv1.SchemaField{
			Name:        f.Name,
			Label:       f.Label,
			Type:        string(f.Type),
			Required:    f.Required,
			Secret:      f.Secret,
			Placeholder: f.Placeholder,
			Help:        f.Help,
			Group:       f.Group,
			DefaultValue: f.Default,
		}
		for _, c := range f.Choices {
			pf.Choices = append(pf.Choices, &payv1.SchemaOption{Value: c.Value, Label: c.Label})
		}
		out.Fields = append(out.Fields, pf)
	}
	return out
}

// CreateMerchantAccount validates the channel config against its schema
// and records the account with the config sealed whole (ADR 0018).
func (s *PayApplication) CreateMerchantAccount(ctx context.Context, req *payv1.CreateMerchantAccountRequest) (*payv1.MerchantAccount, error) {
	channel := payentity.Channel(req.GetChannel())
	if !channel.Valid() {
		err := pay.ErrMerchantInvalid
		logx.Propagate(ctx, s.logger, "pay.create_merchant_account", err)
		return nil, err
	}
	m, err := s.merchants.Create(ctx, &payservice.CreateInput{
		Channel: channel,
		Name:    req.GetName(),
		Config:  req.GetConfig(),
		Remark:  req.GetRemark(),
	}, s.actor(ctx))
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.create_merchant_account", err)
		return nil, err
	}
	return convertMerchant(m), nil
}

// ListMerchantAccounts lists Merchant Accounts with AIP filter/order and
// page-token pagination. Every row reads through its masked echo only.
func (s *PayApplication) ListMerchantAccounts(ctx context.Context, req *payv1.ListMerchantAccountsRequest) (*payv1.MerchantAccountSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("name", filtering.TypeString),
		filtering.DeclareIdent("channel", filtering.TypeInt),
		filtering.DeclareIdent("status", filtering.TypeInt),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_merchant_accounts", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_merchant_accounts", err)
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_merchant_accounts", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_merchant_accounts", err)
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		orderBy.Fields = []ordering.Field{{Path: "id", Desc: true}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	merchants, err := s.merchants.List(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_merchant_accounts", err)
		return nil, err
	}
	set := &payv1.MerchantAccountSet{Merchants: make([]*payv1.MerchantAccount, 0, len(merchants))}
	for _, m := range merchants {
		set.Merchants = append(set.Merchants, convertMerchant(m))
	}
	if len(merchants) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// GetMerchantAccount retrieves one Merchant Account (masked config only).
func (s *PayApplication) GetMerchantAccount(ctx context.Context, req *payv1.GetMerchantAccountRequest) (*payv1.MerchantAccount, error) {
	m, err := s.merchants.Get(ctx, req.GetMerchantId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.get_merchant_account", err)
		return nil, err
	}
	return convertMerchant(m), nil
}

// UpdateMerchantAccount replaces the display fields and the whole config
// form; secret fields left blank keep their stored values (ADR 0021).
func (s *PayApplication) UpdateMerchantAccount(ctx context.Context, req *payv1.UpdateMerchantAccountRequest) (*payv1.MerchantAccount, error) {
	m, err := s.merchants.Update(ctx, &payservice.UpdateInput{
		ID:     req.GetMerchantId(),
		Name:   req.GetName(),
		Config: req.GetConfig(),
		Remark: req.GetRemark(),
	}, s.actor(ctx))
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.update_merchant_account", err)
		return nil, err
	}
	return convertMerchant(m), nil
}

// ChangeMerchantAccountStatus enables or disables a Merchant Account.
func (s *PayApplication) ChangeMerchantAccountStatus(ctx context.Context, req *payv1.ChangeMerchantAccountStatusRequest) (*payv1.MerchantAccount, error) {
	m, err := s.merchants.ChangeStatus(ctx, req.GetMerchantId(), payentity.MerchantStatus(req.GetStatus()), s.actor(ctx))
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.change_merchant_account_status", err)
		return nil, err
	}
	return convertMerchant(m), nil
}

// WithdrawMerchantAccount soft-deletes the Merchant Account.
func (s *PayApplication) WithdrawMerchantAccount(ctx context.Context, req *payv1.WithdrawMerchantAccountRequest) (*emptypb.Empty, error) {
	if err := s.merchants.Withdraw(ctx, req.GetMerchantId(), s.actor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "pay.withdraw_merchant_account", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ProbeMerchantAccount runs the Connectivity Test: one cheap authenticated
// Channel call, moving no money.
func (s *PayApplication) ProbeMerchantAccount(ctx context.Context, req *payv1.ProbeMerchantAccountRequest) (*payv1.ProbeResult, error) {
	outcome, err := s.merchants.Probe(ctx, req.GetMerchantId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.probe_merchant_account", err)
		return nil, err
	}
	return &payv1.ProbeResult{
		Ok:        outcome.OK,
		Message:   outcome.Message,
		LatencyMs: outcome.LatencyMS,
	}, nil
}

// GetChannelSchemas returns every adopted channel's config schema — the
// source the merchant form renders from.
func (s *PayApplication) GetChannelSchemas(ctx context.Context, req *payv1.GetChannelSchemasRequest) (*payv1.GetChannelSchemasResponse, error) {
	schemas := s.merchants.Schemas()
	out := &payv1.GetChannelSchemasResponse{Schemas: make([]*payv1.ChannelSchema, 0, len(schemas))}
	for _, schema := range schemas {
		out.Schemas = append(out.Schemas, convertSchema(schema, true))
	}
	return out, nil
}

// ChannelSecretFields lists every adopted channel's schema-marked secret
// config field names — the pay half of the operlog scrub set (ADR 0018).
// Nil-tolerant so a pay-less server assembly still boots.
func (s *PayApplication) ChannelSecretFields() []string {
	if s == nil || s.merchants == nil {
		return nil
	}
	fields := make([]string, 0, 8)
	for _, schema := range s.merchants.Schemas() {
		for _, f := range schema.Fields {
			if f.Secret {
				fields = append(fields, f.Name)
			}
		}
	}
	return fields
}
