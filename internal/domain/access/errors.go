// Package access owns the access module per ADR 0005: Menus, Roles, Menu
// Grants, and Role Assignments, together with the Authorization Resolution
// capability. The package root holds only the typed errors; the objects live
// in entity, the ports in repository, and the module's own rules in service.
package access

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
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
)

var (
	// ErrRouteAccessUnavailable: route resolution (which feeds the
	// authenticated GetRouters endpoint) could not read its backing store.
	// It is the access-owned twin of authn's AUTH_BACKEND_UNAVAILABLE with an
	// identical wire shape (code, reason, message), so the endpoint's 503
	// contract is unchanged while access depends on no authn type — the same
	// wire-compatibility ruling as access's ErrUserNotFound.
	ErrRouteAccessUnavailable = errors.ServiceUnavailable("AUTH_BACKEND_UNAVAILABLE", "authentication backend unavailable")
)

var (
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

var (
	// ErrUserNotFound: the System User named by a Role Assignment operation
	// does not exist (or is soft-deleted). Access owns this copy of the user
	// module's reason because the assignment endpoints are access's; the
	// client-visible contract is identical.
	ErrUserNotFound = errors.NotFound("USER_NOT_FOUND", "user not found")
)

var (
	// ErrAuthorizationUnavailable: resolution could not read its backing
	// store. The reason is internal to access; consumers map it onto their
	// own availability vocabulary, so it never reaches a client as-is.
	ErrAuthorizationUnavailable = errors.New(500, "AUTHORIZATION_UNAVAILABLE", "authorization resolution unavailable")
	// ErrAccessStoreUnavailable: an access-store operation behind the Menu,
	// Role, or Role Assignment repos failed unexpectedly — driver down, tx
	// failure, serialization. Adapters log the raw error once and return this
	// sentinel (ADR 0007), so no driver text crosses the repository boundary;
	// clients see 503, never a driver message.
	ErrAccessStoreUnavailable = errors.ServiceUnavailable("ACCESS_STORE_UNAVAILABLE", "access store unavailable")
)
