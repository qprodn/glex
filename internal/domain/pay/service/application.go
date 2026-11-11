package service

import (
	"context"
	"log/slog"
	"net"
	"net/url"
	"strings"

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
// display name, and any result address the caller is replacing. A nil
// address leaves the stored value unchanged. A non-nil address, including
// empty, replaces it. The number and the Merchant Account binding are not
// fields of this request.
type UpdateApplicationRequest struct {
	ID   int64
	Name string
	// OrderNotifyURL is nil when the caller is not changing the payment
	// result address.
	OrderNotifyURL *string
	// RefundNotifyURL is nil when the caller is not changing the refund
	// result address.
	RefundNotifyURL *string
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
	// Update changes the display name and any result address present on the
	// request. The number and the Merchant Account binding stay as recorded.
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

// sharedAddressSpace is RFC 6598 carrier-grade NAT. net.IP.IsPrivate does
// not cover it, and a result address must not dial into it.
var sharedAddressSpace = net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

func validateApplicationName(name string) error {
	if name == "" || len(name) > 64 {
		return pay.ErrApplicationInvalid
	}
	return nil
}

// DisallowedNotifyIP reports an address a Business Notification must not
// dial: loopback, private, shared, link-local, multicast, or unspecified.
func DisallowedNotifyIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil && sharedAddressSpace.Contains(v4) {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// validateNotifyURL accepts an empty address (no notice) or an absolute
// http(s) URL whose host is a name or a public IP. Loopback, private, and
// link-local hosts are refused. A hostname is checked again when it is dialed.
func validateNotifyURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > maxNotifyURLLen || strings.ContainsAny(raw, " \t\r\n") {
		return pay.ErrApplicationInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil {
		return pay.ErrApplicationInvalid
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return pay.ErrApplicationInvalid
	}
	host := u.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") {
		return pay.ErrApplicationInvalid
	}
	if ip := net.ParseIP(host); ip != nil && DisallowedNotifyIP(ip) {
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
	if err := validateApplicationName(req.Name); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
		return nil, err
	}
	if req.OrderNotifyURL != nil {
		if err := validateNotifyURL(*req.OrderNotifyURL); err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
			return nil, err
		}
	}
	if req.RefundNotifyURL != nil {
		if err := validateNotifyURL(*req.RefundNotifyURL); err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
			return nil, err
		}
	}
	app, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.application.update", err)
		return nil, err
	}
	app.Name = req.Name
	if req.OrderNotifyURL != nil {
		app.OrderNotifyURL = *req.OrderNotifyURL
	}
	if req.RefundNotifyURL != nil {
		app.RefundNotifyURL = *req.RefundNotifyURL
	}
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
	if _, err := s.apps.FindByID(ctx, req.ID); err != nil {
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
