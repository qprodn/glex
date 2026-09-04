package authn

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	authnv1 "glex/api/authn/v1"
	"glex/internal/crossdomain/access"
	auditcontract "glex/internal/crossdomain/audit"
	auditmodel "glex/internal/crossdomain/audit/model"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	"glex/pkg/logx"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/mileusna/useragent"
	"google.golang.org/protobuf/types/known/emptypb"
)

// LoginRequest contains credentials and authoritative request metadata for
// the login operation. Password exists only while the operation verifies it.
type LoginRequest struct {
	UserName  string
	Password  string
	SourceIP  string
	UserAgent string
}

// LoginApplication implements the authn-owned proto AuthService: the
// Login/Logout endpoints. It owns the module's cross-domain orchestration:
// the login transaction boundary, the Authorization Snapshot filled through
// the access crossdomain contract, and the commit-failure compensation. All
// repository access goes through the login domain service (ADR 0005);
// authentication policy is enforced by the registered HTTP middleware.
type LoginApplication struct {
	authnv1.UnimplementedAuthServiceServer

	login      authnservice.Login
	authorizer access.AuthorizationResolver
	loginLogs  auditcontract.LoginLogWriter
	tx         Transaction
	logger     *slog.Logger
	now        func() time.Time
}

// NewLoginApplication creates a LoginApplication. login is the login domain
// service — the module's only repository holder (account protocol, failure
// counters, session store); authorizer fills the Authorization Snapshot
// through the access crossdomain contract (login never derives permissions
// itself, ADR 0005); loginLogs records the Login Log through the audit
// crossdomain contract, best-effort (ADR 0012); tx owns the login
// transaction boundary.
func NewLoginApplication(login authnservice.Login, authorizer access.AuthorizationResolver, loginLogs auditcontract.LoginLogWriter, tx Transaction, logger *slog.Logger) *LoginApplication {
	return &LoginApplication{
		login:      login,
		authorizer: authorizer,
		loginLogs:  loginLogs,
		tx:         tx,
		logger:     logger.With(slog.String("component", "application.authn.login")),
		now:        time.Now,
	}
}

// Login creates one Login Session and returns its bearer credential exactly
// once. Request metadata is read at this transport boundary; the credentials
// and the orchestration live in login below. Both outcomes record a Login
// Log row, best-effort (ADR 0012).
func (s *LoginApplication) Login(ctx context.Context, req *authnv1.LoginRequest) (*authnv1.LoginReply, error) {
	sourceIP, userAgent := requestMetadata(ctx)
	result, err := s.authenticate(ctx, &LoginRequest{
		UserName:  req.GetUsername(),
		Password:  req.GetPassword(),
		SourceIP:  sourceIP,
		UserAgent: userAgent,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.login", err)
		return nil, err
	}
	return &authnv1.LoginReply{
		AccessToken: result.AccessToken,
		ExpireIn:    int64(result.ExpiresIn / time.Second),
	}, nil
}

// requestMetadata reads the authoritative client metadata (forwarded source
// IP and User-Agent) at the transport boundary.
func requestMetadata(ctx context.Context) (sourceIP, userAgent string) {
	if transporter, ok := transport.FromServerContext(ctx); ok && transporter.RequestHeader() != nil {
		sourceIP = strings.TrimSpace(transporter.RequestHeader().Get("X-Forwarded-For"))
		userAgent = transporter.RequestHeader().Get("User-Agent")
	}
	return sourceIP, userAgent
}

// Logout succeeds anonymously and otherwise revokes only the current Login
// Session attached by OPTIONAL authentication. A successful logout records
// its Login Log row best-effort (ADR 0012).
func (s *LoginApplication) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	principal, _ := authnservice.PrincipalFromContext(ctx)
	if principal == nil {
		return &emptypb.Empty{}, nil
	}
	sourceIP, userAgent := requestMetadata(ctx)
	if err := s.login.Logout(ctx, principal.SessionID()); err != nil {
		logx.Propagate(ctx, s.logger, "authn.logout", err)
		return nil, err
	}
	_, browser, os := loginClientMetadata(userAgent)
	s.recordLoginLog(ctx, &auditmodel.LoginLog{
		UserID:   principal.SystemUserID(),
		UserName: principal.UserName(),
		IPAddr:   sourceIP,
		Browser:  browser,
		OS:       os,
		Success:  true,
		Msg:      "退出登录",
		ActorID:  principal.SystemUserID(),
	})
	return &emptypb.Empty{}, nil
}

// login verifies credentials without revealing whether the account exists,
// enforces the two failure counters, validates account state only after the
// password matches, and delegates token/snapshot creation to the login
// service. Credential issuance is deliberately the last transaction step; a
// commit failure after it is compensated by discarding the credential.
func (s *LoginApplication) authenticate(ctx context.Context, req *LoginRequest) (*authnentity.LoginResult, error) {
	userName := strings.TrimSpace(req.UserName)
	account, err := s.login.VerifyPassword(ctx, userName, req.Password, req.SourceIP)
	if err != nil {
		s.recordLoginFailure(ctx, req, 0, err)
		return nil, err
	}
	// Clear before issuing a bearer credential: a Redis failure cannot leave an
	// unreturned but live Login Session behind. The login service revalidates
	// the password hash and status inside the Application transaction.
	if err := s.login.ClearFailures(ctx, userName); err != nil {
		return nil, err
	}
	deviceType, browser, os := loginClientMetadata(req.UserAgent)
	loginTime := s.now()
	var result *authnentity.LoginResult
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		locked, err := s.login.Revalidate(ctx, account.ID, account.UserName, account.PasswordHash)
		if err != nil {
			return err
		}
		snapshot, err := s.loadAuthorization(ctx, locked)
		if err != nil {
			return err
		}
		if err := s.login.RecordLogin(ctx, locked.ID, req.SourceIP, loginTime); err != nil {
			return err
		}
		// Credential issuance is deliberately last. A commit failure after a
		// successful issue is compensated by discarding the credential below.
		result, err = s.login.IssueCredential(ctx, snapshot, &authnentity.LoginSession{
			SystemUserID: locked.ID,
			UserName:     locked.UserName,
			DeviceType:   deviceType,
			IPAddress:    req.SourceIP,
			Browser:      browser,
			OS:           os,
			LoginTime:    loginTime,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, authndomain.ErrInvalidCredentials) {
			mapped := s.login.RecordFailure(ctx, userName, req.SourceIP)
			s.recordLoginFailure(ctx, req, 0, mapped)
			return nil, mapped
		}
		if result != nil {
			// Every Redis write succeeded, so the failure is the commit:
			// tear the credential down (best-effort — a leftover token
			// expires on its own). DiscardCredential is best-effort by
			// contract: each failed cleanup step is logged-and-mapped at the
			// infra exit that owns it (ADR 0007), so ignoring the returned
			// error here adds no new information.
			_ = s.login.DiscardCredential(ctx, account.ID, result.AccessToken)
		}
		s.recordLoginFailure(ctx, req, account.ID, err)
		return nil, err
	}
	s.recordLoginSuccess(ctx, req, account.ID, account.UserName)
	return result, nil
}

// recordLoginSuccess persists the success row of one sign-in, best-effort
// (ADR 0012): a recording failure logs Warn at this handling site and never
// changes the login result.
func (s *LoginApplication) recordLoginSuccess(ctx context.Context, req *LoginRequest, userID int64, userName string) {
	_, browser, os := loginClientMetadata(req.UserAgent)
	s.recordLoginLog(ctx, &auditmodel.LoginLog{
		UserID:   userID,
		UserName: userName,
		IPAddr:   req.SourceIP,
		Browser:  browser,
		OS:       os,
		Success:  true,
		Msg:      "登录成功",
		ActorID:  userID,
	})
}

// recordLoginFailure persists the failure row of one sign-in attempt. The
// attempted name is always carried; the account id stays 0 — the failure
// deliberately reveals no account existence (anti-enumeration), and neither
// does the log.
func (s *LoginApplication) recordLoginFailure(ctx context.Context, req *LoginRequest, userID int64, cause error) {
	_, browser, os := loginClientMetadata(req.UserAgent)
	s.recordLoginLog(ctx, &auditmodel.LoginLog{
		UserID:   userID,
		UserName: strings.TrimSpace(req.UserName),
		IPAddr:   req.SourceIP,
		Browser:  browser,
		OS:       os,
		Success:  false,
		Msg:      truncate(cause.Error(), 256),
	})
}

// recordLoginLog writes one Login Log row through the audit crossdomain
// contract. It is the handling site for recording failures (ADR 0007): the
// write is best-effort (ADR 0012) — the contract's adapter detaches the
// context and bounds it, so a recording problem can only ever log.
func (s *LoginApplication) recordLoginLog(ctx context.Context, l *auditmodel.LoginLog) {
	if err := s.loginLogs.RecordLoginLog(ctx, l); err != nil {
		s.logger.WarnContext(ctx, "login log record failed",
			slog.String("operation", "authn.record_login_log"),
			slog.Any("error", err),
		)
	}
}

// truncate caps a string at n runes so a cause text can never overflow the
// msg column.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// loadAuthorization fills the Authorization Snapshot's authorization content
// through the access crossdomain contract — login never derives permissions
// itself (ADR 0005). The carrier fields (identity) are authn's own; any
// resolution failure is the authorization backend being unavailable.
func (s *LoginApplication) loadAuthorization(ctx context.Context, locked *authnentity.LoginAccount) (*authnentity.AuthorizationSnapshot, error) {
	resolution, err := s.authorizer.ResolveAuthorization(ctx, locked.ID)
	if err != nil {
		return nil, authndomain.ErrAuthBackendUnavailable
	}
	snapshot := &authnentity.AuthorizationSnapshot{
		SystemUserID: locked.ID,
		UserName:     locked.UserName,
	}
	if resolution != nil {
		snapshot.RoleKeys = resolution.RoleKeys
		snapshot.Permissions = resolution.Permissions
	}
	return snapshot, nil
}

func loginClientMetadata(raw string) (deviceType, browser, os string) {
	ua := useragent.Parse(raw)
	switch {
	case ua.Bot:
		deviceType = "bot"
	case ua.Tablet:
		deviceType = "tablet"
	case ua.Mobile:
		deviceType = "mobile"
	case ua.Desktop:
		deviceType = "desktop"
	default:
		deviceType = "unknown"
	}
	browser = ua.Name
	if browser == "" {
		browser = "Unknown"
	}
	os = ua.OS
	if os == "" {
		os = "Unknown"
	}
	return deviceType, browser, os
}
