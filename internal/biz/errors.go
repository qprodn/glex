package biz

import (
	menuv1 "glex/api/system/menu/v1"
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

	// ErrMenuNotFound: the requested menu does not exist.
	ErrMenuNotFound = errors.NotFound(menuv1.ErrorReason_MENU_NOT_FOUND.String(), "menu not found")
	// ErrMenuHasChildren: the menu still has children and cannot be deleted.
	ErrMenuHasChildren = errors.BadRequest(menuv1.ErrorReason_MENU_HAS_CHILDREN.String(), "menu has children")
	// ErrMenuNameConflict: menu_name already exists under the same parent.
	ErrMenuNameConflict = errors.Conflict(menuv1.ErrorReason_MENU_NAME_CONFLICT.String(), "menu_name already exists under this parent")
	// ErrMenuParentInvalid: a menu cannot be its own parent.
	ErrMenuParentInvalid = errors.BadRequest(menuv1.ErrorReason_MENU_PARENT_INVALID.String(), "a menu cannot be its own parent")
	// ErrMenuTypeInvalid: menu_type is not a valid MenuType.
	ErrMenuTypeInvalid = errors.BadRequest(menuv1.ErrorReason_MENU_TYPE_INVALID.String(), "invalid menu_type")
)
