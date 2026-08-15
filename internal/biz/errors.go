package biz

import (
	menuv1 "glex/api/system/menu/v1"
	rolev1 "glex/api/system/role/v1"
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

	// ErrRoleNotFound: the requested role does not exist.
	ErrRoleNotFound = errors.NotFound(rolev1.ErrorReason_ROLE_NOT_FOUND.String(), "role not found")
	// ErrRoleNameConflict: role_name already exists.
	ErrRoleNameConflict = errors.Conflict(rolev1.ErrorReason_ROLE_NAME_CONFLICT.String(), "role_name already exists")
	// ErrRoleKeyConflict: role_key already exists.
	ErrRoleKeyConflict = errors.Conflict(rolev1.ErrorReason_ROLE_KEY_CONFLICT.String(), "role_key already exists")
	// ErrRoleInvalid: role_name/role_key is blank.
	ErrRoleInvalid = errors.BadRequest(rolev1.ErrorReason_ROLE_INVALID.String(), "role_name and role_key are required")
	// ErrRoleMenuNotFound: one or more menu ids in a Menu Grant do not exist
	// (or are soft-deleted).
	ErrRoleMenuNotFound = errors.BadRequest(rolev1.ErrorReason_ROLE_MENU_NOT_FOUND.String(), "one or more menu ids do not exist")
	// ErrRoleAssigned: the role still has users assigned and cannot be
	// deleted or disabled (RuoYi: 已分配，不能删除/禁用).
	ErrRoleAssigned = errors.BadRequest(rolev1.ErrorReason_ROLE_ASSIGNED.String(), "role is assigned to users and cannot be deleted or disabled")
)
