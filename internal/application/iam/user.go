package iam

import (
	"context"
	"log/slog"

	userv1 "glex/api/iam/user/v1"
	authndomain "glex/internal/domain/authn"
	iamdomain "glex/internal/domain/iam"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertUser maps a iamdomain.User DO to the proto User DTO. Password is deliberately
// omitted — it is write-only (accepted on create/update, never returned).
func convertUser(m *iamdomain.User) *userv1.User {
	u := &userv1.User{
		UserId:      m.ID,
		UserName:    m.UserName,
		NickName:    m.NickName,
		Email:       m.Email,
		PhoneNumber: m.PhoneNumber,
		Gender:      m.Gender,
		Status:      m.Status,
		Avatar:      m.Avatar,
		Remark:      m.Remark,
		CreateBy:    m.CreateBy,
		UpdateBy:    m.UpdateBy,
		LoginIp:     m.LoginIP,
		CreatedAt:   timestamppb.New(m.CreatedAt),
		UpdatedAt:   timestamppb.New(m.UpdatedAt),
	}
	if m.LoginDate != nil {
		u.LoginDate = timestamppb.New(*m.LoginDate)
	}
	return u
}

// UserApplication implements the system-user CRUD service.
//
// Authentication and the operation's Functional Permission policy are
// enforced by the registered HTTP middleware. This adapter remains limited to
// DTO/DO conversion and keeps authorization and mutation rules out of service.
type UserApplication struct {
	userv1.UnimplementedUserServiceServer

	uc     userUsecase
	logger *slog.Logger
}

type userUsecase interface {
	CreateUser(context.Context, *iamdomain.User) (*iamdomain.User, error)
	GetUser(context.Context, int64) (*iamdomain.User, error)
	GetCurrentUserInfo(context.Context, *authndomain.Principal) (*iamdomain.CurrentUserInfo, error)
	ListUsers(context.Context, ...iamdomain.ListOption) ([]*iamdomain.User, error)
	UpdateUser(context.Context, *iamdomain.User) (*iamdomain.User, error)
	DeleteUser(context.Context, int64) error
}

// NewUserApplication creates a UserApplication.
func NewUserApplication(uc *UserUsecase, logger *slog.Logger) *UserApplication {
	return newUserApplication(uc, logger)
}

func newUserApplication(uc userUsecase, logger *slog.Logger) *UserApplication {
	return &UserApplication{
		uc:     uc,
		logger: logger.With(slog.String("component", "application.user")),
	}
}

// CreateUser creates a system user.
func (s *UserApplication) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.User, error) {
	in := req.GetUser()
	created, err := s.uc.CreateUser(ctx, &iamdomain.User{
		UserName:    in.GetUserName(),
		NickName:    in.GetNickName(),
		Email:       in.GetEmail(),
		PhoneNumber: in.GetPhoneNumber(),
		Gender:      in.GetGender(),
		Password:    in.GetPassword(),
		Status:      in.GetStatus(),
		Avatar:      in.GetAvatar(),
		Remark:      in.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return convertUser(created), nil
}

// GetUser retrieves a system user by id.
func (s *UserApplication) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.User, error) {
	u, err := s.uc.GetUser(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	return convertUser(u), nil
}

// GetInfo returns the freshly loaded current System User plus the Role keys and
// Functional Permissions authenticated from the shared Authorization Snapshot.
func (s *UserApplication) GetInfo(ctx context.Context, _ *emptypb.Empty) (*userv1.CurrentUserInfo, error) {
	principal, ok := authndomain.PrincipalFromContext(ctx)
	if !ok {
		return nil, authndomain.ErrCredentialMissing
	}
	info, err := s.uc.GetCurrentUserInfo(ctx, principal)
	if err != nil {
		return nil, err
	}
	u := info.User
	current := &userv1.CurrentUser{
		UserId:      u.ID,
		UserName:    u.UserName,
		NickName:    u.NickName,
		Email:       u.Email,
		PhoneNumber: u.PhoneNumber,
		Gender:      u.Gender,
		Status:      u.Status,
		Avatar:      u.Avatar,
		Remark:      u.Remark,
		CreateBy:    u.CreateBy,
		UpdateBy:    u.UpdateBy,
		LoginIp:     u.LoginIP,
		CreatedAt:   timestamppb.New(u.CreatedAt),
		UpdatedAt:   timestamppb.New(u.UpdatedAt),
	}
	if u.LoginDate != nil {
		current.LoginDate = timestamppb.New(*u.LoginDate)
	}
	return &userv1.CurrentUserInfo{
		User:        current,
		RoleKeys:    append([]string(nil), info.RoleKeys...),
		Permissions: append([]string(nil), info.Permissions...),
	}, nil
}

// ListUsers lists users with AIP-160 filter, AIP-132 order, and page-token
// pagination.
func (s *UserApplication) ListUsers(ctx context.Context, req *userv1.ListUsersRequest) (*userv1.UserSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("email", filtering.TypeString),
		filtering.DeclareIdent("phone_number", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	)
	if err != nil {
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	users, err := s.uc.ListUsers(ctx,
		iamdomain.ListFilter(filter),
		iamdomain.ListOrderBy(orderBy),
		iamdomain.ListLimit(int(req.GetPageSize())),
		iamdomain.ListOffset(int(pageToken.Offset)),
	)
	if err != nil {
		return nil, err
	}
	set := &userv1.UserSet{Users: make([]*userv1.User, 0, len(users))}
	for _, u := range users {
		set.Users = append(set.Users, convertUser(u))
	}
	if len(users) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// UpdateUser partially updates a user via field-mask. An empty password in the
// masked request means "leave unchanged".
func (s *UserApplication) UpdateUser(ctx context.Context, req *userv1.UpdateUserRequest) (*userv1.User, error) {
	// Load current state as a DTO, apply the mask onto it, then persist. The DTO
	// carries no password, so a password outside the mask stays empty and the
	// usecase treats it as "unchanged".
	current, err := s.GetUser(ctx, &userv1.GetUserRequest{UserId: req.GetUser().GetUserId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetUser())
	updated, err := s.uc.UpdateUser(ctx, &iamdomain.User{
		ID:          current.GetUserId(),
		UserName:    current.GetUserName(),
		NickName:    current.GetNickName(),
		Email:       current.GetEmail(),
		PhoneNumber: current.GetPhoneNumber(),
		Gender:      current.GetGender(),
		Password:    current.GetPassword(),
		Status:      current.GetStatus(),
		Avatar:      current.GetAvatar(),
		Remark:      current.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return convertUser(updated), nil
}

// DeleteUser soft-deletes a user by id.
func (s *UserApplication) DeleteUser(ctx context.Context, req *userv1.DeleteUserRequest) (*emptypb.Empty, error) {
	if err := s.uc.DeleteUser(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
