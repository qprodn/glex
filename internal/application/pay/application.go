package pay

import (
	"context"

	payv1 "glex/api/pay/v1"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertPayApplication maps a domain Pay Application to the proto DTO.
func convertPayApplication(app *payentity.PayApplication) *payv1.PayApplication {
	return &payv1.PayApplication{
		ApplicationId:   app.ID,
		Name:            app.Name,
		MerchantId:      app.MerchantID,
		Status:          int32(app.Status),
		OrderNotifyUrl:  app.OrderNotifyURL,
		RefundNotifyUrl: app.RefundNotifyURL,
		CreateBy:        app.CreateBy,
		UpdateBy:        app.UpdateBy,
		CreatedAt:       timestamppb.New(app.CreatedAt),
		UpdatedAt:       timestamppb.New(app.UpdatedAt),
	}
}

// CreatePayApplication binds a new Pay Application to one enabled Merchant
// Account. Either result address may be empty.
func (s *PayApplication) CreatePayApplication(ctx context.Context, req *payv1.CreatePayApplicationRequest) (*payv1.PayApplication, error) {
	app, err := s.applications.Create(ctx, payservice.CreateApplicationRequest{
		Name:            req.GetName(),
		MerchantID:      req.GetMerchantId(),
		OrderNotifyURL:  req.GetOrderNotifyUrl(),
		RefundNotifyURL: req.GetRefundNotifyUrl(),
		ActorID:         s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.create_pay_application", err)
		return nil, err
	}
	return convertPayApplication(app), nil
}

// applicationListSpec is this module's list fact (ADR 0024): filterable
// idents plus the default listing order. merchant_id and status are the
// operator filters; name is the display-name lookup.
var applicationListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("name", filtering.TypeString),
		filtering.DeclareIdent("merchant_id", filtering.TypeInt),
		filtering.DeclareIdent("status", filtering.TypeInt),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListPayApplications lists Pay Applications that are not withdrawn.
func (s *PayApplication) ListPayApplications(ctx context.Context, req *payv1.ListPayApplicationsRequest) (*payv1.PayApplicationSet, error) {
	q, page, err := listq.Parse(req, applicationListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_pay_applications", err)
		return nil, err
	}
	apps, err := s.applications.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.list_pay_applications", err)
		return nil, err
	}
	set := &payv1.PayApplicationSet{Applications: make([]*payv1.PayApplication, 0, len(apps))}
	for _, app := range apps {
		set.Applications = append(set.Applications, convertPayApplication(app))
	}
	set.NextPageToken = page.NextToken(len(apps))
	return set, nil
}

// GetPayApplication retrieves one Pay Application that is not withdrawn.
func (s *PayApplication) GetPayApplication(ctx context.Context, req *payv1.GetPayApplicationRequest) (*payv1.PayApplication, error) {
	app, err := s.applications.Get(ctx, req.GetApplicationId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.get_pay_application", err)
		return nil, err
	}
	return convertPayApplication(app), nil
}

// UpdatePayApplication changes the display name and either result address.
// The Merchant Account binding is not a field of this request.
func (s *PayApplication) UpdatePayApplication(ctx context.Context, req *payv1.UpdatePayApplicationRequest) (*payv1.PayApplication, error) {
	app, err := s.applications.Update(ctx, payservice.UpdateApplicationRequest{
		ID:              req.GetApplicationId(),
		Name:            req.GetName(),
		OrderNotifyURL:  req.GetOrderNotifyUrl(),
		RefundNotifyURL: req.GetRefundNotifyUrl(),
		ActorID:         s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.update_pay_application", err)
		return nil, err
	}
	return convertPayApplication(app), nil
}

// ChangePayApplicationStatus enables or disables a Pay Application.
func (s *PayApplication) ChangePayApplicationStatus(ctx context.Context, req *payv1.ChangePayApplicationStatusRequest) (*payv1.PayApplication, error) {
	app, err := s.applications.ChangeStatus(ctx, payservice.ChangeApplicationStatusRequest{
		ID:      req.GetApplicationId(),
		Status:  payentity.ApplicationStatus(req.GetStatus()),
		ActorID: s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.change_pay_application_status", err)
		return nil, err
	}
	return convertPayApplication(app), nil
}

// WithdrawPayApplication soft-deletes the Pay Application.
func (s *PayApplication) WithdrawPayApplication(ctx context.Context, req *payv1.WithdrawPayApplicationRequest) (*emptypb.Empty, error) {
	if err := s.applications.Withdraw(ctx, payservice.WithdrawApplicationRequest{
		ID:      req.GetApplicationId(),
		ActorID: s.actor(ctx),
	}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.withdraw_pay_application", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
