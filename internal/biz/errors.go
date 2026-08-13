package biz

import (
	userv1 "glex/api/system/user/v1"
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrUserNotFound: the requested user does not exist.
	ErrUserNotFound = errors.NotFound(userv1.ErrorReason_USER_NOT_FOUND.String(), "user not found")
	// ErrUserNameConflict: user_name already taken.
	ErrUserNameConflict = errors.Conflict(userv1.ErrorReason_USER_NAME_CONFLICT.String(), "user_name already exists")
	// ErrEmailConflict: email already taken.
	ErrEmailConflict = errors.Conflict(userv1.ErrorReason_EMAIL_CONFLICT.String(), "email already exists")
	// ErrPhoneConflict: phone_number already taken.
	ErrPhoneConflict = errors.Conflict(userv1.ErrorReason_PHONE_CONFLICT.String(), "phone_number already exists")
	// ErrPasswordRequired: create called without a password.
	ErrPasswordRequired = errors.BadRequest(userv1.ErrorReason_PASSWORD_REQUIRED.String(), "password is required")
)
