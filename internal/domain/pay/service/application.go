package service

import (
	"context"
	"log/slog"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// CreateApplicationRequest is what a Pay Application create states: the
// display name, the Merchant Account it binds, the result addresses (either
// may be empty), and the actor stamped into the audit fields.
type CreateApplicationRequest struct {
	Name            string
	MerchantID      int64
	OrderNotifyURL  string
	RefundNotifyURL string
	// ActorID stamps create_by/update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// UpdateApplicationRequest is what a Pay Application edit states: the
// display name and both result addresses. The number and the Merchant
// Account binding are not fields of this request.
type UpdateApplicationRequest struct {
	ID              int64
	Name            string
	OrderNotifyURL  string
	RefundNotifyURL string
	// ActorID stamps update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// WithdrawApplicationRequest is one Pay Application withdrawal.
type WithdrawApplicationRequest struct {
	ID int64
	// ActorID stamps update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// ChangeApplicationStatusRequest is one enable-switch flip.
type ChangeApplicationStatusRequest struct {
	ID     int64
	Status payentity.ApplicationStatus
	// ActorID stamps update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// Application is the Pay Application domain service.
type Application interface {
	// Create binds a new Pay Application to exactly one Merchant Account
	// and records it enabled.
	Create(ctx context.Context, req CreateApplicationRequest) (*payentity.PayApplication, error)
	// Update changes the display name and either result address. The number
	// and the Merchant Account binding stay as recorded.
	Update(ctx context.Context, req UpdateApplicationRequest) (*payentity.PayApplication, error)
	// ChangeStatus enables or disables a Pay Application.
	ChangeStatus(ctx context.Context, req ChangeApplicationStatusRequest) (*payentity.PayApplication, error)
	// Withdraw soft-deletes the Pay Application so it leaves the live set.
	Withdraw(ctx context.Context, req WithdrawApplicationRequest) error
	// Get resolves one Pay Application that is not withdrawn.
	Get(ctx context.Context, id int64) (*payentity.PayApplication, error)
	// List returns Pay Applications that are not withdrawn.
	List(ctx context.Context, q listq.Query) ([]*payentity.PayApplication, error)
}

type applicationImpl struct {
	apps      payrepo.PayApplicationRepo
	merchants payrepo.MerchantAccountRepo
	logger    *slog.Logger
}

// NewApplicationService creates the Pay Application domain service.
func NewApplicationService(
	apps payrepo.PayApplicationRepo,
	merchants payrepo.MerchantAccountRepo,
	logger *slog.Logger,
) Application {
	return &applicationImpl{
		apps:      apps,
		merchants: merchants,
		logger:    logger.With(slog.String("component", "domain.pay.application.svc")),
	}
}

const maxNotifyURLLen = 512

func validateApplicationName(name string) error {
	if name == "" || len(name) > 64 {
		return pay.ErrApplicationInvalid
	}
	return nil
}

func validateNotifyURL(url string) error {
	if len(url) > maxNotifyURLLen {
		return pay.ErrApplicationInvalid
	}
	return nil
}

func validateApplicationFields(name, orderURL, refundURL string) error {
	if err := validateApplicationName(name); err != nil {
		return err
	}
	if err := validateNotifyURL(orderURL); err != nil {
		return err
	}
	return validateNotifyURL(refundURL)
}

func (s *applicationImpl) Create(ctx context.Context, req CreateApplicationRequest) (*payentity.PayApplication, error) {
	if err := validateApplicationFields(req.Name, req.OrderNotifyURL, req.RefundNotifyURL); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.create", err)
		return nil, err
	}
	m, err := s.merchants.FindByID(ctx, req.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.create", err)
		return nil, err
	}
	if m.Status != payentity.MerchantEnabled {
		err = pay.ErrMerchantInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.application.create", err)
		return nil, err
	}
	app := &payentity.PayApplication{
		ID:              s.apps.NextID(),
		Name:            req.Name,
		MerchantID:      req.MerchantID,
		Status:          payentity.ApplicationEnabled,
		OrderNotifyURL:  req.OrderNotifyURL,
		RefundNotifyURL: req.RefundNotifyURL,
		CreateBy:        req.ActorID,
		UpdateBy:        req.ActorID,
	}
	created, err := s.apps.Create(ctx, app)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.create", err)
		return nil, err
	}
	return created, nil
}

func (s *applicationImpl) Update(ctx context.Context, req UpdateApplicationRequest) (*payentity.PayApplication, error) {
	if err := validateApplicationFields(req.Name, req.OrderNotifyURL, req.RefundNotifyURL); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
		return nil, err
	}
	app, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
		return nil, err
	}
	app.Name = req.Name
	app.OrderNotifyURL = req.OrderNotifyURL
	app.RefundNotifyURL = req.RefundNotifyURL
	updated, err := s.apps.Update(ctx, payrepo.UpdatePayApplicationRequest{App: app, ActorID: req.ActorID})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
		return nil, err
	}
	return updated, nil
}

func (s *applicationImpl) ChangeStatus(ctx context.Context, req ChangeApplicationStatusRequest) (*payentity.PayApplication, error) {
	if req.Status != payentity.ApplicationEnabled && req.Status != payentity.ApplicationDisabled {
		err := pay.ErrApplicationInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.application.change_status", err)
		return nil, err
	}
	if err := s.apps.UpdateStatus(ctx, payrepo.UpdatePayApplicationStatusRequest{
		ID: req.ID, Status: req.Status, ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.change_status", err)
		return nil, err
	}
	app, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.change_status", err)
		return nil, err
	}
	return app, nil
}

func (s *applicationImpl) Withdraw(ctx context.Context, req WithdrawApplicationRequest) error {
	if err := s.apps.Delete(ctx, payrepo.DeletePayApplicationRequest{ID: req.ID, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.withdraw", err)
		return err
	}
	return nil
}

func (s *applicationImpl) Get(ctx context.Context, id int64) (*payentity.PayApplication, error) {
	app, err := s.apps.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.get", err)
		return nil, err
	}
	return app, nil
}

func (s *applicationImpl) List(ctx context.Context, q listq.Query) ([]*payentity.PayApplication, error) {
	apps, err := s.apps.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.list", err)
		return nil, err
	}
	return apps, nil
}
