package biz

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mileusna/useragent"
)

// RoleAssignment is one role with whether the user is assigned it — the auth
// module's read shape for a user's Role Assignment (角色分配), mirroring
// RuoYi's authRole dialog data (all roles + checked flag).
type RoleAssignment struct {
	Role     *Role
	Assigned bool
}

// UserRoleRepo is the repository interface for user↔role links (the inversion
// seam declared in biz, implemented in data). The link table is sys_user_role
// (RuoYi shape: composite PK (user_id, role_id), no timestamps, no soft
// delete — rows are hard-deleted on full replacement and user delete).
type UserRoleRepo interface {
	// ListRoleAssignments returns every active role with whether the user is
	// assigned it, ordered by (role_sort, role_id).
	ListRoleAssignments(ctx context.Context, userID int64) ([]*RoleAssignment, error)
	// ReplaceUserRoles atomically replaces the user's Role Assignment with
	// roleIDs (full replacement; an empty list clears all). roleIDs are stored
	// verbatim — validated and deduped by the usecase first.
	ReplaceUserRoles(ctx context.Context, userID int64, roleIDs []int64) error
	// ListRoleUsers lists the active users assigned the role, matching the
	// given options (filter/order/offset/limit).
	ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*User, error)
	// CountUserRoles counts the active users assigned the role — the guard
	// behind "a role with users cannot be deleted or disabled" (RuoYi:
	// 已分配，不能删除/禁用).
	CountUserRoles(ctx context.Context, roleID int64) (int, error)
}

// AuthUsecase holds the user↔role assignment business rules (auth module).
type AuthUsecase struct {
	userRepo    UserRepo
	roleRepo    RoleRepo
	linkRepo    UserRoleRepo
	loginRepo   AuthLoginRepo
	counterRepo LoginCounterRepo
	now         func() time.Time
}

// NewAuthUsecase creates an AuthUsecase. userRepo/roleRepo back existence
// checks; linkRepo owns the sys_user_role rows.
func NewAuthUsecase(userRepo UserRepo, roleRepo RoleRepo, linkRepo UserRoleRepo, loginRepo AuthLoginRepo, counterRepo LoginCounterRepo) *AuthUsecase {
	return &AuthUsecase{
		userRepo:    userRepo,
		roleRepo:    roleRepo,
		linkRepo:    linkRepo,
		loginRepo:   loginRepo,
		counterRepo: counterRepo,
		now:         time.Now,
	}
}

// Login verifies credentials without revealing whether the System User exists,
// enforces the two failure counters, validates System User state only after bcrypt
// succeeds, and delegates token/snapshot creation to the data wrapper.
func (uc *AuthUsecase) Login(ctx context.Context, req *LoginRequest) (*LoginResult, error) {
	if req == nil || uc.loginRepo == nil || uc.counterRepo == nil {
		return nil, ErrInvalidCredentials
	}
	userName := strings.TrimSpace(req.UserName)
	if remaining, err := uc.counterRepo.LockRemaining(ctx, userName, req.SourceIP); err != nil {
		return nil, err
	} else if remaining > 0 {
		return nil, NewLoginLocked(remaining)
	}

	u, err := uc.userRepo.FindByUserName(ctx, userName)
	if err != nil {
		if !errors.Is(err, ErrUserNotFound) {
			return nil, err
		}
		_ = passwordMatches(invalidCredentialHash, req.Password)
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	if !passwordMatches(u.Password, req.Password) {
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	if u.Status != 0 {
		return nil, ErrSystemUserDisabled
	}
	// Clear before issuing a bearer credential: a Redis failure cannot leave an
	// unreturned but live Login Session behind. The data wrapper revalidates the
	// password hash and status under the authorization lock.
	if err := uc.counterRepo.ClearUserName(ctx, userName); err != nil {
		return nil, err
	}
	deviceType, browser, os := loginClientMetadata(req.UserAgent)
	result, err := uc.loginRepo.Issue(ctx, &LoginIssue{
		SystemUserID: u.ID,
		UserName:     u.UserName,
		PasswordHash: u.Password,
		SourceIP:     req.SourceIP,
		DeviceType:   deviceType,
		Browser:      browser,
		OS:           os,
		LoginTime:    uc.now(),
	})
	if errors.Is(err, ErrInvalidCredentials) {
		return nil, uc.invalidLogin(ctx, userName, req.SourceIP)
	}
	return result, err
}

func (uc *AuthUsecase) invalidLogin(ctx context.Context, userName, sourceIP string) error {
	remaining, err := uc.counterRepo.RecordFailure(ctx, userName, sourceIP)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return NewLoginLocked(remaining)
	}
	return ErrInvalidCredentials
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
func (uc *AuthUsecase) ListUserRoles(ctx context.Context, userID int64) ([]*RoleAssignment, error) {
	if _, err := uc.userRepo.FindByID(ctx, userID); err != nil {
		return nil, err
	}
	return uc.linkRepo.ListRoleAssignments(ctx, userID)
}

// UpdateUserRoles replaces the user's Role Assignment: role_ids are validated
// (exist, not soft-deleted) and deduped, then stored as a full replacement
// (an empty list clears all). Returns the updated assignment.
func (uc *AuthUsecase) UpdateUserRoles(ctx context.Context, userID int64, roleIDs []int64) ([]*RoleAssignment, error) {
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
	if err := uc.linkRepo.ReplaceUserRoles(ctx, userID, unique); err != nil {
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
func (uc *AuthUsecase) ListRoleUsers(ctx context.Context, roleID int64, opts ...ListOption) ([]*User, error) {
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
func (uc *AuthUsecase) assertRolesAssignable(ctx context.Context, assignments []*RoleAssignment, roleIDs []int64) ([]int64, error) {
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
			return nil, ErrRoleNotFound
		}
	}
	return unique, nil
}
