package authn

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"glex/internal/crossdomain/access"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
	authnservice "glex/internal/domain/authn/service"

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
	login       authnrepo.AuthLoginBackend
	counterRepo authnrepo.LoginCounterRepo
	sessionRepo authnrepo.LoginSessionRepo
	authorizer  access.AuthorizationResolver
	tx          Transaction
	logger      *slog.Logger
	now         func() time.Time
}

// NewLoginUsecase creates a LoginUsecase. login backs the account protocol
// (lookup, revalidation, credential issuance); counterRepo owns the two login
// failure counters; sessionRepo backs Logout; authorizer fills the
// Authorization Snapshot through the access crossdomain contract (login never
// derives permissions itself, ADR 0016/0017); tx owns the login transaction
// boundary.
func NewLoginUsecase(login authnrepo.AuthLoginBackend, counterRepo authnrepo.LoginCounterRepo, sessionRepo authnrepo.LoginSessionRepo, authorizer access.AuthorizationResolver, tx Transaction, logger *slog.Logger) *LoginUsecase {
	return &LoginUsecase{
		login:       login,
		counterRepo: counterRepo,
		sessionRepo: sessionRepo,
		authorizer:  authorizer,
		tx:          tx,
		logger:      logger.With(slog.String("component", "application.authn.login")),
		now:         time.Now,
	}
}

// Logout revokes only the authenticated Principal's current Login Session. An
// anonymous call is the idempotent success path admitted by OPTIONAL auth.
func (uc *LoginUsecase) Logout(ctx context.Context, principal *authnentity.Principal) error {
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
func (uc *LoginUsecase) Login(ctx context.Context, req *LoginRequest) (*authnentity.LoginResult, error) {
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
		_ = authnservice.VerifyPassword(authnservice.DecoyCredentialHash, req.Password)
		return nil, authnservice.RecordInvalidLogin(ctx, uc.counterRepo, userName, req.SourceIP)
	}
	if !authnservice.VerifyPassword(account.PasswordHash, req.Password) {
		return nil, authnservice.RecordInvalidLogin(ctx, uc.counterRepo, userName, req.SourceIP)
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
	var result *authnentity.LoginResult
	err = uc.tx.InTx(ctx, func(ctx context.Context) error {
		locked, err := uc.login.Revalidate(ctx, account.ID, account.UserName, account.PasswordHash)
		if err != nil {
			return err
		}
		snapshot, err := uc.loadAuthorization(ctx, locked)
		if err != nil {
			return err
		}
		if err := uc.login.RecordLogin(ctx, locked.ID, req.SourceIP, loginTime); err != nil {
			return err
		}
		// Credential issuance is deliberately last. A commit failure after a
		// successful issue is compensated by discarding the credential below.
		result, err = uc.login.IssueCredential(ctx, snapshot, &authnentity.LoginSession{
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
			return nil, authnservice.RecordInvalidLogin(ctx, uc.counterRepo, userName, req.SourceIP)
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

// loadAuthorization fills the Authorization Snapshot's authorization content
// through the access crossdomain contract — login never derives permissions
// itself (ADR 0016). The carrier fields (identity) are authn's own; any
// resolution failure is the authorization backend being unavailable.
func (uc *LoginUsecase) loadAuthorization(ctx context.Context, locked *authnentity.LoginAccount) (*authnentity.AuthorizationSnapshot, error) {
	resolution, err := uc.authorizer.ResolveAuthorization(ctx, locked.ID)
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
