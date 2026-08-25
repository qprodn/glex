package iam

import (
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrLoginSessionNotFound: the Session ID is absent or expired.
	ErrLoginSessionNotFound = errors.NotFound("TOKEN_INVALID", "Login Session not found")
	// ErrLoginSessionNotOwned: a System User attempted to remove another
	// System User's Login Session.
	ErrLoginSessionNotOwned = errors.Forbidden("PERMISSION_DENIED", "Login Session belongs to another System User")
	// Authentication-state errors are mapped by data so no sa-token or Redis
	// error crosses the repository boundary.
	ErrTokenInvalid           = errors.Unauthorized("TOKEN_INVALID", "invalid token")
	ErrCredentialMissing      = errors.Unauthorized("CREDENTIAL_MISSING", "Authorization credential is required")
	ErrCredentialMalformed    = errors.Unauthorized("CREDENTIAL_MALFORMED", "Authorization credential is malformed")
	ErrTokenExpired           = errors.Unauthorized("TOKEN_EXPIRED", "token expired")
	ErrTokenIdleTimeout       = errors.Unauthorized("TOKEN_IDLE_TIMEOUT", "Login Session idle timeout")
	ErrLoginSessionKickedOut  = errors.Unauthorized("SESSION_KICKED_OUT", "Login Session kicked out")
	ErrLoginSessionReplaced   = errors.Unauthorized("SESSION_REPLACED", "Login Session replaced")
	ErrAuthBackendUnavailable = errors.ServiceUnavailable("AUTH_BACKEND_UNAVAILABLE", "authentication backend unavailable")
	ErrPermissionDenied       = errors.Forbidden("PERMISSION_DENIED", "Functional Permission denied")
	ErrInvalidCredentials     = errors.Unauthorized("INVALID_CREDENTIALS", "invalid username or password")
	ErrSystemUserDisabled     = errors.Forbidden("ACCOUNT_DISABLED", "System User is disabled")

	// ErrUserNotFound: the requested user does not exist.
	ErrUserNotFound = errors.NotFound("USER_NOT_FOUND", "user not found")
	// ErrUserNameAmbiguous means an accepted best-effort uniqueness race left
	// more than one active System User with the same login identifier.
	ErrUserNameAmbiguous = errors.Conflict("USER_NAME_AMBIGUOUS", "user_name identifies multiple users")
	// ErrUserNameConflict: user_name already taken.
	ErrUserNameConflict = errors.Conflict("USER_NAME_CONFLICT", "user_name already exists")
	// ErrEmailConflict: email already taken.
	ErrEmailConflict = errors.Conflict("EMAIL_CONFLICT", "email already exists")
	// ErrPhoneConflict: phone_number already taken.
	ErrPhoneConflict = errors.Conflict("PHONE_CONFLICT", "phone_number already exists")
	// ErrPasswordRequired: create called without a password.
	ErrPasswordRequired = errors.BadRequest("PASSWORD_REQUIRED", "password is required")

	// ErrMenuNotFound: the requested menu does not exist.
	ErrMenuNotFound = errors.NotFound("MENU_NOT_FOUND", "menu not found")
	// ErrMenuHasChildren: the menu still has children and cannot be deleted.
	ErrMenuHasChildren = errors.BadRequest("MENU_HAS_CHILDREN", "menu has children")
	// ErrMenuNameConflict: menu_name already exists under the same parent.
	ErrMenuNameConflict = errors.Conflict("MENU_NAME_CONFLICT", "menu_name already exists under this parent")
	// ErrMenuParentInvalid: a menu cannot be its own parent.
	ErrMenuParentInvalid = errors.BadRequest("MENU_PARENT_INVALID", "a menu cannot be its own parent")
	// ErrMenuTypeInvalid: menu_type is not a valid MenuType.
	ErrMenuTypeInvalid = errors.BadRequest("MENU_TYPE_INVALID", "invalid menu_type")

	// ErrRoleNotFound: the requested role does not exist.
	ErrRoleNotFound = errors.NotFound("ROLE_NOT_FOUND", "role not found")
	// ErrRoleNameConflict: role_name already exists.
	ErrRoleNameConflict = errors.Conflict("ROLE_NAME_CONFLICT", "role_name already exists")
	// ErrRoleKeyConflict: role_key already exists.
	ErrRoleKeyConflict = errors.Conflict("ROLE_KEY_CONFLICT", "role_key already exists")
	// ErrRoleInvalid: role_name/role_key is blank.
	ErrRoleInvalid = errors.BadRequest("ROLE_INVALID", "role_name and role_key are required")
	// ErrRoleMenuNotFound: one or more menu ids in a Menu Grant do not exist
	// (or are soft-deleted).
	ErrRoleMenuNotFound = errors.BadRequest("ROLE_MENU_NOT_FOUND", "one or more menu ids do not exist")
	// ErrRoleAssigned: the role still has users assigned and cannot be
	// deleted or disabled (RuoYi: 已分配，不能删除/禁用).
	ErrRoleAssigned = errors.BadRequest("ROLE_ASSIGNED", "role is assigned to users and cannot be deleted or disabled")
)

// NewLoginLocked returns the stable LOGIN_LOCKED reason together with a
// rounded-up remaining duration. The metadata does not identify which counter
// (username or source IP) caused the lock.
func NewLoginLocked(remaining time.Duration) error {
	seconds := int64((remaining + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return errors.Unauthorized("LOGIN_LOCKED", "login temporarily locked").
		WithMetadata(map[string]string{"remaining_seconds": strconv.FormatInt(seconds, 10)})
}
