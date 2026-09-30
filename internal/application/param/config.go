package param

import (
	"context"
	"log/slog"

	paramv1 "glex/api/param/v1"
	"glex/internal/bizpkg"
	paramentity "glex/internal/domain/param/entity"
	paramservice "glex/internal/domain/param/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertConfig maps a paramentity.Config DO to the proto Config DTO.
func convertConfig(c *paramentity.Config) *paramv1.Config {
	return &paramv1.Config{
		ConfigId:    c.ID,
		ConfigName:  c.ConfigName,
		ConfigKey:   c.ConfigKey,
		ConfigValue: c.ConfigValue,
		BuiltIn:     c.BuiltIn,
		Remark:      c.Remark,
		CreateBy:    c.CreateBy,
		UpdateBy:    c.UpdateBy,
		CreatedAt:   timestamppb.New(c.CreatedAt),
		UpdatedAt:   timestamppb.New(c.UpdatedAt),
	}
}

// ConfigApplication implements the runtime-parameter management service. It
// owns the module's transaction boundary; all repository access goes through
// the param domain service, where the intra-domain rules live too (ADR 0005 —
// this module has no cross-domain orchestration). Authentication and the
// operation's Functional Permission policy are enforced by the registered
// HTTP middleware.
type ConfigApplication struct {
	paramv1.UnimplementedConfigServiceServer

	svc    paramservice.Configs
	tx     Transaction
	logger *slog.Logger
}

// NewConfigApplication creates a ConfigApplication. svc is the param domain
// service — the module's only repository holder; tx wraps the writes.
func NewConfigApplication(svc paramservice.Configs, tx Transaction, logger *slog.Logger) *ConfigApplication {
	return &ConfigApplication{
		svc:    svc,
		tx:     tx,
		logger: logger.With(slog.String("component", "application.param")),
	}
}

// CreateConfig creates a runtime parameter. config_name must be non-blank and
// config_key must match ^[A-Za-z0-9][A-Za-z0-9._-]*$ and be unique among
// active parameters.
func (s *ConfigApplication) CreateConfig(ctx context.Context, req *paramv1.CreateConfigRequest) (*paramv1.Config, error) {
	in := req.GetConfig()
	c := &paramentity.Config{
		ConfigName:  in.GetConfigName(),
		ConfigKey:   in.GetConfigKey(),
		ConfigValue: in.GetConfigValue(),
		BuiltIn:     in.GetBuiltIn(),
		Remark:      in.GetRemark(),
	}
	actor := bizpkg.AuditActor(ctx)
	c.CreateBy, c.UpdateBy = actor, actor
	var created *paramentity.Config
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.Create(ctx, c)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "param.create_config", err)
		return nil, err
	}
	return convertConfig(created), nil
}

// GetConfig retrieves a runtime parameter by id.
func (s *ConfigApplication) GetConfig(ctx context.Context, req *paramv1.GetConfigRequest) (*paramv1.Config, error) {
	c, err := s.svc.Get(ctx, req.GetConfigId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.get_config", err)
		return nil, err
	}
	return convertConfig(c), nil
}

// The param module's list facts (ADR 0024): the filterable idents plus the
// newest-first listing order the console defaults to.
var configListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("config_name", filtering.TypeString),
		filtering.DeclareIdent("config_key", filtering.TypeString),
		filtering.DeclareIdent("built_in", filtering.TypeBool),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListConfigs lists runtime parameters with AIP-160 filter, AIP-132 order,
// and page-token pagination.
func (s *ConfigApplication) ListConfigs(ctx context.Context, req *paramv1.ListConfigsRequest) (*paramv1.ConfigSet, error) {
	q, page, err := listq.Parse(req, configListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.list_configs", err)
		return nil, err
	}
	configs, err := s.svc.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.list_configs", err)
		return nil, err
	}
	set := &paramv1.ConfigSet{Configs: make([]*paramv1.Config, 0, len(configs))}
	for _, c := range configs {
		set.Configs = append(set.Configs, convertConfig(c))
	}
	set.NextPageToken = page.NextToken(len(configs))
	return set, nil
}

// UpdateConfig partially updates a runtime parameter via field-mask. It loads
// the current state as a DTO, applies the mask onto it, then persists. All
// business fields are updatable — the key included: a Config key stays
// editable after create (unlike a Dict Type key, ADR 0006), and the service's
// uniqueness pre-check covers renames the same way it covers creates.
func (s *ConfigApplication) UpdateConfig(ctx context.Context, req *paramv1.UpdateConfigRequest) (*paramv1.Config, error) {
	current, err := s.GetConfig(ctx, &paramv1.GetConfigRequest{ConfigId: req.GetConfig().GetConfigId()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "param.update_config", err)
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetConfig())
	c := &paramentity.Config{
		ID:          current.GetConfigId(),
		ConfigName:  current.GetConfigName(),
		ConfigKey:   current.GetConfigKey(),
		ConfigValue: current.GetConfigValue(),
		BuiltIn:     current.GetBuiltIn(),
		Remark:      current.GetRemark(),
		UpdateBy:    bizpkg.AuditActor(ctx),
	}
	var updated *paramentity.Config
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.svc.Update(ctx, c)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "param.update_config", err)
		return nil, err
	}
	return convertConfig(updated), nil
}

// DeleteConfig soft-deletes a runtime parameter by id; refused for built-in
// parameters (RuoYi: 内置参数不能删除).
func (s *ConfigApplication) DeleteConfig(ctx context.Context, req *paramv1.DeleteConfigRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		return s.svc.Delete(ctx, &paramservice.DeleteRequest{
			ID:      req.GetConfigId(),
			ActorID: bizpkg.AuditActor(ctx),
		})
	}); err != nil {
		logx.Propagate(ctx, s.logger, "param.delete_config", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
