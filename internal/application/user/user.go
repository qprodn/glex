package user

import (
	"context"
	"log/slog"

	userv1 "glex/api/user/v1"
	"glex/internal/bizpkg"
	"glex/internal/conf"
	"glex/internal/crossdomain/authn"
	authnmodel "glex/internal/crossdomain/authn/model"
	"glex/internal/crossdomain/dict"
	"glex/internal/crossdomain/file"
	"glex/internal/crossdomain/org"
	authndomain "glex/internal/domain/authn"
	authnservice "glex/internal/domain/authn/service"
	userdomain "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	userservice "glex/internal/domain/user/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
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
		DeptId:      m.DeptID,
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

// fillAvatarURLs fills the read-only avatar_url on converted users with one
// batched Display URL lookup per response (ADR 0016). A failed lookup
// degrades to the default avatar — the contract boundary and the domain
// service's breadcrumbs already recorded why, so an avatar never fails the
// user read carrying it.
func (s *UserApplication) fillAvatarURLs(ctx context.Context, users ...*userv1.User) {
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		if u.GetAvatar() != 0 {
			ids = append(ids, u.GetAvatar())
		}
	}
	if len(ids) == 0 {
		return
	}
	urls := s.resolveAvatarURLs(ctx, ids)
	for _, u := range users {
		u.AvatarUrl = urls[u.GetAvatar()]
	}
}

// avatarURL resolves one avatar id to its Display URL; unset ids and any
// failure yield "" (same degrade rule as fillAvatarURLs).
func (s *UserApplication) avatarURL(ctx context.Context, fileID int64) string {
	if fileID == 0 {
		return ""
	}
	return s.resolveAvatarURLs(ctx, []int64{fileID})[fileID]
}

// resolveAvatarURLs is the single crossdomain ask behind every avatar_url:
// one batched lookup, errors swallowed to empty after the boundary logged
// them.
func (s *UserApplication) resolveAvatarURLs(ctx context.Context, ids []int64) map[int64]string {
	urls, err := s.avatars.ResolveURLs(ctx, ids)
	if err != nil {
		return nil
	}
	return urls
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

	svc          userservice.User
	credentials  authn.CredentialWriter
	depts        org.DeptReader
	avatars      file.URLResolver
	importFiles  file.ContentReader
	dicts        dict.VocabularyReader
	initPassword string
	tx           Transaction
	revoker      authn.SessionRevoker
	logger       *slog.Logger
}

// NewUserApplication creates a UserApplication. svc is the System User domain
// service — the module's only repository holder; credentials stores password
// requests through the authn crossdomain contract (the credential column is
// authn's, per ADR 0005); tx wraps security writes together with their
// session revocation; revoker performs the revocation through the authn
// crossdomain contract; importFiles streams Import Files in and dicts
// translates sheet columns (ADR 0017); initPassword is the Data Import
// initial credential (empty = DefaultImportInitPassword).
func NewUserApplication(svc userservice.User, credentials authn.CredentialWriter, depts org.DeptReader, avatars file.URLResolver, importFiles file.ContentReader, dicts dict.VocabularyReader, cfg *conf.User, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *UserApplication {
	initPassword := cfg.GetInitPassword()
	if initPassword == "" {
		initPassword = DefaultImportInitPassword
	}
	return &UserApplication{
		svc:          svc,
		credentials:  credentials,
		depts:        depts,
		avatars:      avatars,
		importFiles:  importFiles,
		dicts:        dicts,
		initPassword: initPassword,
		tx:           tx,
		revoker:      revoker,
		logger:       logger.With(slog.String("component", "application.user")),
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
		DeptID:      in.GetDeptId(),
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
		if err := s.ensureDept(ctx, u.DeptID); err != nil {
			return err
		}
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
	out := convertUser(created)
	s.fillAvatarURLs(ctx, out)
	return out, nil
}

// GetUser retrieves a system user by id.
func (s *UserApplication) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.User, error) {
	u, err := s.svc.Get(ctx, req.GetUserId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.get_user", err)
		return nil, err
	}
	out := convertUser(u)
	s.fillAvatarURLs(ctx, out)
	return out, nil
}

// GetInfo returns the freshly loaded current System User plus the Role keys and
// Functional Permissions authenticated from the shared Authorization Snapshot.
func (s *UserApplication) GetInfo(ctx context.Context, _ *emptypb.Empty) (*userv1.CurrentUserInfo, error) {
	principal := authnservice.PrincipalFromContext(ctx)
	if principal == nil {
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
		DeptId:      u.DeptID,
		CreateBy:    u.CreateBy,
		UpdateBy:    u.UpdateBy,
		LoginIp:     u.LoginIP,
		CreatedAt:   timestamppb.New(u.CreatedAt),
		UpdatedAt:   timestamppb.New(u.UpdatedAt),
	}
	if u.LoginDate != nil {
		current.LoginDate = timestamppb.New(*u.LoginDate)
	}
	current.AvatarUrl = s.avatarURL(ctx, current.GetAvatar())
	return &userv1.CurrentUserInfo{
		User:        current,
		RoleKeys:    append([]string(nil), principal.RoleKeys()...),
		Permissions: append([]string(nil), principal.Permissions()...),
	}, nil
}

// userListSpec is this module's list fact (ADR 0024): filterable idents
// plus the listing order — newest first, pinned here because the old
// stanza left requests without an order at the database's mercy.
var userListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("email", filtering.TypeString),
		filtering.DeclareIdent("phone_number", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	},
	DefaultOrder: []ordering.Field{{Path: "id", Desc: true}},
}

// ListUsers lists users with AIP-160 filter, AIP-132 order, and page-token
// pagination.
func (s *UserApplication) ListUsers(ctx context.Context, req *userv1.ListUsersRequest) (*userv1.UserSet, error) {
	q, page, err := listq.Parse(req, userListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_users", err)
		return nil, err
	}
	var deptIDs []int64
	if req.GetDeptId() != 0 {
		deptIDs, err = s.depts.ResolveSubtreeIDs(ctx, req.GetDeptId())
		if err != nil {
			logx.Propagate(ctx, s.logger, "user.list_users", err)
			return nil, err
		}
	}
	users, err := s.svc.List(ctx, userservice.ListUsersRequest{Query: q, DeptIDs: deptIDs})
	if err != nil {
		logx.Propagate(ctx, s.logger, "user.list_users", err)
		return nil, err
	}
	set := &userv1.UserSet{Users: make([]*userv1.User, 0, len(users))}
	for _, u := range users {
		set.Users = append(set.Users, convertUser(u))
	}
	s.fillAvatarURLs(ctx, set.Users...)
	set.NextPageToken = page.NextToken(len(users))
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
		DeptID:      current.GetDeptId(),
		UpdateBy:    bizpkg.AuditActor(ctx),
	}
	passwordPolicy := userentity.RevokeAllLoginSessions()
	if u.Password != "" {
		if principal := authnservice.PrincipalFromContext(ctx); principal != nil && principal.SystemUserID() == u.ID {
			passwordPolicy = userentity.PreserveLoginSession(principal.SessionID())
		}
	}
	var updated *userentity.User
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.ensureDept(ctx, u.DeptID); err != nil {
			return err
		}
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
	out := convertUser(updated)
	s.fillAvatarURLs(ctx, out)
	return out, nil
}

// DeleteUser soft-deletes a user by id and currently revokes their Login
// Sessions in the same transaction.
func (s *UserApplication) DeleteUser(ctx context.Context, req *userv1.DeleteUserRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := s.svc.Delete(ctx, userservice.DeleteUserRequest{ID: req.GetUserId(), ActorID: bizpkg.AuditActor(ctx)})
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

// ensureDept validates a non-empty department reference through the org
// crossdomain contract (ADR 0013): the department must exist and be enabled.
// A zero dept_id (no department) passes. org's kratos errors (DEPT_NOT_FOUND,
// DEPT_DISABLED) cross to the caller verbatim — the wire shape is the
// contract's.
func (s *UserApplication) ensureDept(ctx context.Context, deptID int64) error {
	if deptID == 0 {
		return nil
	}
	return s.depts.EnsureActiveDept(ctx, deptID)
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
