package iam

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	iamdomain "glex/internal/domain/iam"

	"github.com/mileusna/useragent"
	"golang.org/x/crypto/bcrypt"
)

// LoginRequest contains credentials and authoritative request metadata for
// the login operation. Password exists only while the operation verifies it.
type LoginRequest struct {
	UserName  string
	Password  string
	SourceIP  string
	UserAgent string
}

// AuthUsecase holds the user↔role assignment business rules (auth module).
type AuthUsecase struct {
	userRepo    iamdomain.UserRepo
	roleRepo    iamdomain.RoleRepo
	linkRepo    iamdomain.UserRoleRepo
	login       iamdomain.AuthLoginBackend
	counterRepo iamdomain.LoginCounterRepo
	sessionRepo iamdomain.LoginSessionRepo
	tx          Transaction
	logger      *slog.Logger
	now         func() time.Time
}

// NewAuthUsecase creates an AuthUsecase. userRepo/roleRepo back existence
// checks; linkRepo owns the sys_user_role rows; tx owns the login protocol and
// Role Assignment replacement transaction boundaries.
func NewAuthUsecase(userRepo iamdomain.UserRepo, roleRepo iamdomain.RoleRepo, linkRepo iamdomain.UserRoleRepo, login iamdomain.AuthLoginBackend, counterRepo iamdomain.LoginCounterRepo, sessionRepo iamdomain.LoginSessionRepo, tx Transaction, logger *slog.Logger) *AuthUsecase {
	return &AuthUsecase{
		userRepo:    userRepo,
		roleRepo:    roleRepo,
		linkRepo:    linkRepo,
		login:       login,
		counterRepo: counterRepo,
		sessionRepo: sessionRepo,
		tx:          tx,
		logger:      logger.With(slog.String("component", "application.iam.auth")),
		now:         time.Now,
	}
}

// Logout revokes only the authenticated iamdomain.Principal's current Login Session. An
// anonymous call is the idempotent success path admitted by OPTIONAL auth.
func (uc *AuthUsecase) Logout(ctx context.Context, principal *iamdomain.Principal) error {
	if principal == nil {
		return nil
	}
	return uc.sessionRepo.Logout(ctx, principal.SessionID())
}

// Login verifies credentials without revealing whether the System iamdomain.User exists,
// enforces the two failure counters, validates System iamdomain.User state only after bcrypt
// succeeds, and delegates token/snapshot creation to the data wrapper.
func (uc *AuthUsecase) Login(ctx context.Context, req *LoginRequest) (*iamdomain.LoginResult, error) {
	if req == nil || uc.login == nil || uc.counterRepo == nil {
		return nil, iamdomain.ErrInvalidCredentials
	}
	userName := strings.TrimSpace(req.UserName)
	if remaining, err := uc.counterRepo.LockRemaining(ctx, userName, req.SourceIP); err != nil {
		return nil, err
	} else if remaining > 0 {
		return nil, iamdomain.NewLoginLocked(remaining)
	}

	u, err := uc.userRepo.FindByUserName(ctx, userName)
	if err != nil {
		if !errors.Is(err, iamdomain.ErrUserNotFound) && !errors.Is(err, iamdomain.ErrUserNameAmbiguous) {
			return nil, err
		}
		_ = passwordMatches(invalidCredentialHash, req.Password)
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	if !passwordMatches(u.Password, req.Password) {
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	if u.Status != 0 {
		return nil, iamdomain.ErrSystemUserDisabled
	}
	// Clear before issuing a bearer credential: a Redis failure cannot leave an
	// unreturned but live Login Session behind. The login repo revalidates the
	// password hash and status inside the Application transaction.
	if err := uc.counterRepo.ClearUserName(ctx, userName); err != nil {
		return nil, err
	}
	deviceType, browser, os := loginClientMetadata(req.UserAgent)
	loginTime := uc.now()
	var result *iamdomain.LoginResult
	err = uc.tx.InTx(ctx, func(ctx context.Context) error {
		locked, err := uc.login.Revalidate(ctx, u.ID, u.UserName, u.Password)
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
		result, err = uc.login.IssueCredential(ctx, snapshot, &iamdomain.LoginSession{
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
		if errors.Is(err, iamdomain.ErrInvalidCredentials) {
			return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
		}
		if result != nil {
			// Every Redis write succeeded, so the failure is the commit:
			// tear the credential down (best-effort — a leftover token
			// expires on its own).
			_ = uc.login.DiscardCredential(ctx, u.ID, result.AccessToken)
		}
		return nil, err
	}
	return result, nil
}

func (uc *AuthUsecase) invalidLogin(ctx context.Context, userName, sourceIP string) error {
	remaining, err := uc.counterRepo.RecordFailure(ctx, userName, sourceIP)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return iamdomain.NewLoginLocked(remaining)
	}
	return iamdomain.ErrInvalidCredentials
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

// ListUserRoles returns every active role with its assigned flag for the
// user — the assign dialog's data in one call. The user must exist (and not
// be soft-deleted).
func (uc *AuthUsecase) ListUserRoles(ctx context.Context, userID int64) ([]*iamdomain.RoleAssignment, error) {
	if _, err := uc.userRepo.FindByID(ctx, userID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleAssignments(ctx, userID)
}

// UpdateUserRoles replaces the user's Role Assignment: role_ids are validated
// (exist, not soft-deleted) and deduped, then stored as a full replacement
// (an empty list clears all) in one transaction that also revokes the user's
// Login Sessions. Redis revocation failure rolling back the PostgreSQL
// replacement is part of this Usecase's business contract. Returns the
// updated assignment.
func (uc *AuthUsecase) UpdateUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]*iamdomain.RoleAssignment, error) {
	if _, err := uc.userRepo.FindByID(ctx, userID); err != nil {
		return nil, err
	}
	assignments, err := uc.linkRepo.ListRoleAssignments(ctx, userID)
	if err != nil {
		return nil, err
	}
	unique, err := uc.assertRolesAssignable(ctx, assignments, roleIDs)
	if err != nil {
		return nil, err
	}
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.linkRepo.ReplaceUserRoles(ctx, userID, unique)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.sessionRepo, affected...)
	}); err != nil {
		return nil, err
	}
	assigned := make(map[int64]struct{}, len(unique))
	for _, id := range unique {
		assigned[id] = struct{}{}
	}
	for _, a := range assignments {
		_, a.Assigned = assigned[a.Role.ID]
	}
	return assignments, nil
}

// ListRoleUsers lists the active users assigned the given role. The role must
// exist (and not be soft-deleted).
func (uc *AuthUsecase) ListRoleUsers(ctx context.Context, roleID int64, opts ...iamdomain.ListOption) ([]*iamdomain.User, error) {
	if _, err := uc.roleRepo.FindByID(ctx, roleID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleUsers(ctx, roleID, opts...)
}

// assertRolesAssignable dedupes roleIDs and checks every id exists and is not
// soft-deleted. The active-role set is the freshly-read assignment list — the
// same data the response carries, so no second query (Menu Grant instead
// re-reads via ListByIDs; here the read is needed anyway for the reply). An
// empty list is legal — it means "no roles" — and passes without touching
// anything.
func (uc *AuthUsecase) assertRolesAssignable(ctx context.Context, assignments []*iamdomain.RoleAssignment, roleIDs []int64) ([]int64, error) {
	unique := dedupeIDs(roleIDs)
	if len(unique) == 0 {
		return unique, nil
	}
	byID := make(map[int64]struct{}, len(assignments))
	for _, a := range assignments {
		byID[a.Role.ID] = struct{}{}
	}
	for _, id := range unique {
		if _, ok := byID[id]; !ok {
			return nil, iamdomain.ErrRoleNotFound
		}
	}
	return unique, nil
}

// invalidCredentialHash makes unknown-user verification consume the same
// bcrypt work as a wrong password without depending on any real System User.
const invalidCredentialHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5jQFKP3N8wJYnAqH.G2L/tW8G8zN2qG"

func passwordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
