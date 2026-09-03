package user

import (
	"context"
	"log/slog"

	userv1 "glex/api/user/v1"
	"glex/internal/bizpkg"
	"glex/internal/crossdomain/authn"
	authnmodel "glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
	authnservice "glex/internal/domain/authn/service"
	userdomain "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	userservice "glex/internal/domain/user/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertUser maps a userentity.User DO to the proto User DTO. Password is deliberately
// omitted — it is write-only (accepted on create/update, never returned).
func convertUser(m *userentity.User) *userv1.User {
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

// UserApplication implements the system-user CRUD service. It owns the
// module's cross-domain orchestration: the transaction boundary, credential
// writes through the authn crossdomain contract, and session revocation. All
// repository access goes through the System User domain service; the
// uniqueness rules live there too (ADR 0005). Authentication and the
// operation's Functional Permission policy are enforced by the registered
// HTTP middleware.
type UserApplication struct {
	userv1.UnimplementedUserServiceServer

	svc         userservice.User
	credentials authn.CredentialWriter
	tx          Transaction
	revoker     authn.SessionRevoker
	logger      *slog.Logger
}

// NewUserApplication creates a UserApplication. svc is the System User domain
// service — the module's only repository holder; credentials stores password
// requests through the authn crossdomain contract (the credential column is
// authn's, per ADR 0005); tx wraps security writes together with their
// session revocation; revoker performs the revocation through the authn
// crossdomain contract.
func NewUserApplication(svc userservice.User, credentials authn.CredentialWriter, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *UserApplication {
	return &UserApplication{
		svc:         svc,
		credentials: credentials,
		tx:          tx,
		revoker:     revoker,
		logger:      logger.With(slog.String("component", "application.user")),
	}
}

// CreateUser creates a system user: it validates uniqueness and persists the
// user together with the initial credential, which authn hashes and stores on
// the same transaction. Uniqueness is a code check against active rows
// (repo-wide convention; the DB has no unique constraints) — racy under
// concurrent creates, accepted.
func (s *UserApplication) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.User, error) {
	in := req.GetUser()
	actor := bizpkg.AuditActor(ctx)
	u := &userentity.User{
		UserName:    in.GetUserName(),
		NickName:    in.GetNickName(),
		Email:       in.GetEmail(),
		PhoneNumber: in.GetPhoneNumber(),
		Gender:      in.GetGender(),
		Password:    in.GetPassword(),
		Status:      in.GetStatus(),
		Avatar:      in.GetAvatar(),
		Remark:      in.GetRemark(),
		CreateBy:    actor,
		UpdateBy:    actor,
	}
	if u.Password == "" {
		err := userdomain.ErrPasswordRequired
		logx.Propagate(ctx, s.logger, "user.create_user", err)
		return nil, err
	}
	var created *userentity.User
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.Create(ctx, u)
		if err != nil {
			return err
		}
		return s.writeCredential(ctx, created.ID, u.Password)
	}); err != nil {
		logx.Propagate(ctx, s.logger, "user.create_user", err)
		return nil, err
	}
	return convertUser(created), nil
}

// GetUser retrieves a system user by id.
func (s *UserApplication) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.User, error) {
	u, err := s.svc.Get(ctx, req.GetUserId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.get_user", err)
		return nil, err
	}
	return convertUser(u), nil
}

// GetInfo returns the freshly loaded current System User plus the Role keys and
// Functional Permissions authenticated from the shared Authorization Snapshot.
func (s *UserApplication) GetInfo(ctx context.Context, _ *emptypb.Empty) (*userv1.CurrentUserInfo, error) {
	principal, ok := authnservice.PrincipalFromContext(ctx)
	if !ok {
		err := authndomain.ErrCredentialMissing
		logx.Propagate(ctx, s.logger, "user.get_info", err)
		return nil, err
	}
	u, err := s.svc.Get(ctx, principal.SystemUserID())
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.get_info", err)
		return nil, err
	}
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
		RoleKeys:    append([]string(nil), principal.RoleKeys()...),
		Permissions: append([]string(nil), principal.Permissions()...),
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
		logx.Propagate(ctx, s.logger, "user.list_users", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_users", err)
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_users", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_users", err)
		return nil, err
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	users, err := s.svc.List(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_users", err)
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
// masked request means "leave unchanged"; a non-empty password is stored
// through authn's credential capability on the same transaction. The mutation
// and the session revocation it classifies currently execute in the same
// transaction.
func (s *UserApplication) UpdateUser(ctx context.Context, req *userv1.UpdateUserRequest) (*userv1.User, error) {
	// Load current state as a DTO, apply the mask onto it, then persist. The DTO
	// carries no password, so a password outside the mask stays empty and the
	// write path treats it as "unchanged".
	current, err := s.GetUser(ctx, &userv1.GetUserRequest{UserId: req.GetUser().GetUserId()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.update_user", err)
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetUser())
	u := &userentity.User{
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
		UpdateBy:    bizpkg.AuditActor(ctx),
	}
	passwordPolicy := userentity.RevokeAllLoginSessions()
	if u.Password != "" {
		if principal, ok := authnservice.PrincipalFromContext(ctx); ok && principal.SystemUserID() == u.ID {
			passwordPolicy = userentity.PreserveLoginSession(principal.SessionID())
		}
	}
	var updated *userentity.User
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := s.svc.Update(ctx, u)
		if err != nil {
			return err
		}
		updated = result.User
		if u.Password != "" {
			if err := s.writeCredential(ctx, u.ID, u.Password); err != nil {
				return err
			}
		}
		// The receiver is the requested change; the persisted record carries
		// no password (authn owns the stored hash), so the classification
		// reads the request's password field.
		policy := u.SessionRevocationFrom(result.Previous, passwordPolicy)
		return revokeSystemUsers(ctx, s.revoker, userentity.SystemUserSessionRevocation{SystemUserID: u.ID, Policy: policy})
	}); err != nil {
		logx.Propagate(ctx, s.logger, "user.update_user", err)
		return nil, err
	}
	return convertUser(updated), nil
}

// DeleteUser soft-deletes a user by id and currently revokes their Login
// Sessions in the same transaction.
func (s *UserApplication) DeleteUser(ctx context.Context, req *userv1.DeleteUserRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := s.svc.Delete(ctx, req.GetUserId(), bizpkg.AuditActor(ctx))
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, s.revoker, affected...)
	}); err != nil {
		logx.Propagate(ctx, s.logger, "user.delete_user", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// writeCredential delegates a password request to authn's credential
// capability. It runs inside the caller's transaction so the profile row and
// the credential commit or roll back together.
func (s *UserApplication) writeCredential(ctx context.Context, systemUserID int64, password string) error {
	return s.credentials.WriteSystemUserCredential(ctx, &authnmodel.SystemUserCredential{
		SystemUserID: systemUserID,
		Password:     password,
	})
}
