package authn

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	authnv1 "glex/api/authn/v1"
	"glex/internal/crossdomain/access"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
	authnservice "glex/internal/domain/authn/service"

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
// the access crossdomain contract, and the commit-failure compensation.
// Intra-domain rules (credential verification, failure-counter normalization)
// live in the domain service; authentication policy is enforced by the
// registered HTTP middleware.
type LoginApplication struct {
	authnv1.UnimplementedAuthServiceServer

	login       authnrepo.AuthLoginBackend
	counterRepo authnrepo.LoginCounterRepo
	sessionRepo authnrepo.LoginSessionRepo
	authorizer  access.AuthorizationResolver
	tx          Transaction
	logger      *slog.Logger
	now         func() time.Time
}

// NewLoginApplication creates a LoginApplication. login backs the account
// protocol (lookup, revalidation, credential issuance); counterRepo owns the
// two login failure counters; sessionRepo backs Logout; authorizer fills the
// Authorization Snapshot through the access crossdomain contract (login never
// derives permissions itself, ADR 0016/0017); tx owns the login transaction
// boundary.
func NewLoginApplication(login authnrepo.AuthLoginBackend, counterRepo authnrepo.LoginCounterRepo, sessionRepo authnrepo.LoginSessionRepo, authorizer access.AuthorizationResolver, tx Transaction, logger *slog.Logger) *LoginApplication {
	return &LoginApplication{
		login:       login,
		counterRepo: counterRepo,
		sessionRepo: sessionRepo,
		authorizer:  authorizer,
		tx:          tx,
		logger:      logger.With(slog.String("component", "application.authn.login")),
		now:         time.Now,
	}
}

// Login creates one Login Session and returns its bearer credential exactly
// once. Request metadata is read at this transport boundary; the credentials
// and the orchestration live in login below.
func (s *LoginApplication) Login(ctx context.Context, req *authnv1.LoginRequest) (*authnv1.LoginReply, error) {
	sourceIP, userAgent := "", ""
	if transporter, ok := transport.FromServerContext(ctx); ok && transporter.RequestHeader() != nil {
		sourceIP = strings.TrimSpace(transporter.RequestHeader().Get("X-Forwarded-For"))
		userAgent = transporter.RequestHeader().Get("User-Agent")
	}
	result, err := s.authenticate(ctx, &LoginRequest{
		UserName:  req.GetUsername(),
		Password:  req.GetPassword(),
		SourceIP:  sourceIP,
		UserAgent: userAgent,
	})
	if err != nil {
		return nil, err
	}
	return &authnv1.LoginReply{
		AccessToken: result.AccessToken,
		ExpireIn:    int64(result.ExpiresIn / time.Second),
	}, nil
}

// Logout succeeds anonymously and otherwise revokes only the current Login
// Session attached by OPTIONAL authentication.
func (s *LoginApplication) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	principal, _ := authnservice.PrincipalFromContext(ctx)
	if principal == nil {
		return &emptypb.Empty{}, nil
	}
	if err := s.sessionRepo.Logout(ctx, principal.SessionID()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// login verifies credentials without revealing whether the account exists,
// enforces the two failure counters, validates account state only after the
// password matches, and delegates token/snapshot creation to the login
// backend. Credential issuance is deliberately the last transaction step; a
// commit failure after it is compensated by discarding the credential.
func (s *LoginApplication) authenticate(ctx context.Context, req *LoginRequest) (*authnentity.LoginResult, error) {
	if req == nil || s.login == nil || s.counterRepo == nil {
		return nil, authndomain.ErrInvalidCredentials
	}
	userName := strings.TrimSpace(req.UserName)
	if remaining, err := s.counterRepo.LockRemaining(ctx, userName, req.SourceIP); err != nil {
		return nil, err
	} else if remaining > 0 {
		return nil, authndomain.NewLoginLocked(remaining)
	}

	account, err := s.login.FindLoginAccount(ctx, userName)
	if err != nil {
		if !errors.Is(err, authndomain.ErrLoginAccountNotFound) && !errors.Is(err, authndomain.ErrLoginAccountAmbiguous) {
			return nil, err
		}
		_ = authnservice.VerifyPassword(authnservice.DecoyCredentialHash, req.Password)
		return nil, authnservice.RecordInvalidLogin(ctx, s.counterRepo, userName, req.SourceIP)
	}
	if !authnservice.VerifyPassword(account.PasswordHash, req.Password) {
		return nil, authnservice.RecordInvalidLogin(ctx, s.counterRepo, userName, req.SourceIP)
	}
	if account.Status != 0 {
		return nil, authndomain.ErrSystemUserDisabled
	}
	// Clear before issuing a bearer credential: a Redis failure cannot leave an
	// unreturned but live Login Session behind. The login backend revalidates
	// the password hash and status inside the Application transaction.
	if err := s.counterRepo.ClearUserName(ctx, userName); err != nil {
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
			return nil, authnservice.RecordInvalidLogin(ctx, s.counterRepo, userName, req.SourceIP)
		}
		if result != nil {
			// Every Redis write succeeded, so the failure is the commit:
			// tear the credential down (best-effort — a leftover token
			// expires on its own).
			_ = s.login.DiscardCredential(ctx, account.ID, result.AccessToken)
		}
		return nil, err
	}
	return result, nil
}

// loadAuthorization fills the Authorization Snapshot's authorization content
// through the access crossdomain contract — login never derives permissions
// itself (ADR 0016). The carrier fields (identity) are authn's own; any
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
