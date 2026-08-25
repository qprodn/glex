package authn

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	authndomain "glex/internal/domain/authn"

	"github.com/mileusna/useragent"
)

// LoginRequest contains credentials and authoritative request metadata for
// the login operation. Password exists only while the operation verifies it.
type LoginRequest struct {
	UserName  string
	Password  string
	SourceIP  string
	UserAgent string
}

// LoginUsecase holds the login/logout business rules (authn module).
type LoginUsecase struct {
	login       authndomain.AuthLoginBackend
	counterRepo authndomain.LoginCounterRepo
	sessionRepo authndomain.LoginSessionRepo
	tx          Transaction
	logger      *slog.Logger
	now         func() time.Time
}

// NewLoginUsecase creates a LoginUsecase. login backs the account protocol
// (lookup, revalidation, Authorization Snapshot, credential issuance);
// counterRepo owns the two login failure counters; sessionRepo backs Logout;
// tx owns the login transaction boundary.
func NewLoginUsecase(login authndomain.AuthLoginBackend, counterRepo authndomain.LoginCounterRepo, sessionRepo authndomain.LoginSessionRepo, tx Transaction, logger *slog.Logger) *LoginUsecase {
	return &LoginUsecase{
		login:       login,
		counterRepo: counterRepo,
		sessionRepo: sessionRepo,
		tx:          tx,
		logger:      logger.With(slog.String("component", "application.authn.login")),
		now:         time.Now,
	}
}

// Logout revokes only the authenticated Principal's current Login Session. An
// anonymous call is the idempotent success path admitted by OPTIONAL auth.
func (uc *LoginUsecase) Logout(ctx context.Context, principal *authndomain.Principal) error {
	if principal == nil {
		return nil
	}
	return uc.sessionRepo.Logout(ctx, principal.SessionID())
}

// Login verifies credentials without revealing whether the account exists,
// enforces the two failure counters, validates account state only after the
// password matches, and delegates token/snapshot creation to the login
// backend. Credential issuance is deliberately the last transaction step; a
// commit failure after it is compensated by discarding the credential.
func (uc *LoginUsecase) Login(ctx context.Context, req *LoginRequest) (*authndomain.LoginResult, error) {
	if req == nil || uc.login == nil || uc.counterRepo == nil {
		return nil, authndomain.ErrInvalidCredentials
	}
	userName := strings.TrimSpace(req.UserName)
	if remaining, err := uc.counterRepo.LockRemaining(ctx, userName, req.SourceIP); err != nil {
		return nil, err
	} else if remaining > 0 {
		return nil, authndomain.NewLoginLocked(remaining)
	}

	account, err := uc.login.FindLoginAccount(ctx, userName)
	if err != nil {
		if !errors.Is(err, authndomain.ErrLoginAccountNotFound) && !errors.Is(err, authndomain.ErrLoginAccountAmbiguous) {
			return nil, err
		}
		_ = authndomain.VerifyPassword(authndomain.DecoyCredentialHash, req.Password)
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	if !authndomain.VerifyPassword(account.PasswordHash, req.Password) {
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	if account.Status != 0 {
		return nil, authndomain.ErrSystemUserDisabled
	}
	// Clear before issuing a bearer credential: a Redis failure cannot leave an
	// unreturned but live Login Session behind. The login backend revalidates
	// the password hash and status inside the Application transaction.
	if err := uc.counterRepo.ClearUserName(ctx, userName); err != nil {
		return nil, err
	}
	deviceType, browser, os := loginClientMetadata(req.UserAgent)
	loginTime := uc.now()
	var result *authndomain.LoginResult
	err = uc.tx.InTx(ctx, func(ctx context.Context) error {
		locked, err := uc.login.Revalidate(ctx, account.ID, account.UserName, account.PasswordHash)
		if err != nil {
			return err
		}
		snapshot, err := uc.login.LoadSnapshot(ctx, locked.ID, locked.UserName)
		if err != nil {
			return err
		}
		if err := uc.login.RecordLogin(ctx, locked.ID, req.SourceIP, loginTime); err != nil {
			return err
		}
		// Credential issuance is deliberately last. A commit failure after a
		// successful issue is compensated by discarding the credential below.
		result, err = uc.login.IssueCredential(ctx, snapshot, &authndomain.LoginSession{
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
			return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
		}
		if result != nil {
			// Every Redis write succeeded, so the failure is the commit:
			// tear the credential down (best-effort — a leftover token
			// expires on its own).
			_ = uc.login.DiscardCredential(ctx, account.ID, result.AccessToken)
		}
		return nil, err
	}
	return result, nil
}

func (uc *LoginUsecase) invalidLogin(ctx context.Context, userName, sourceIP string) error {
	remaining, err := uc.counterRepo.RecordFailure(ctx, userName, sourceIP)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return authndomain.NewLoginLocked(remaining)
	}
	return authndomain.ErrInvalidCredentials
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
