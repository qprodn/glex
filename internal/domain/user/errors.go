// Package user owns the System User module per ADR 0016: the System User
// domain object, its security-change policy, typed errors, and the module's
// ports. The package root holds only the typed errors; the object lives in
// entity, the ports in repository, and the module's own rules in service.
package user

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
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
)
