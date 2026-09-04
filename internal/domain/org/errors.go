// Package org owns the org module per ADR 0005: Departments, Posts, and Post
// Assignments (ADR 0013). The package root holds only the typed errors and
// the root-department constant; the objects live in entity, the ports in
// repository, and the module's own rules in service.
package org

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrDeptNotFound: the requested department does not exist.
	ErrDeptNotFound = errors.NotFound("DEPT_NOT_FOUND", "department not found")
	// ErrDeptHasChildren: the department still has children and cannot be
	// deleted (RuoYi: 存在下级部门,不允许删除).
	ErrDeptHasChildren = errors.BadRequest("DEPT_HAS_CHILDREN", "department has children")
	// ErrDeptHasUsers: the department is still referenced by System Users and
	// cannot be deleted or disabled (RuoYi: 部门存在用户 / 部门下存在已分配用户).
	ErrDeptHasUsers = errors.BadRequest("DEPT_HAS_USERS", "department has users")
	// ErrDeptHasPosts: the department still has posts and cannot be deleted
	// (RuoYi 6.0: 部门存在岗位).
	ErrDeptHasPosts = errors.BadRequest("DEPT_HAS_POSTS", "department has posts")
	// ErrDeptHasEnabledChildren: the department still has enabled descendants
	// and cannot be disabled (RuoYi: 该部门包含未停用的子部门).
	ErrDeptHasEnabledChildren = errors.BadRequest("DEPT_HAS_ENABLED_CHILDREN", "department has enabled children")
	// ErrDeptNameTaken: dept_name already exists under the same parent.
	ErrDeptNameTaken = errors.Conflict("DEPT_NAME_TAKEN", "dept_name already exists under this parent")
	// ErrDeptRootProtected: the seeded root department cannot be deleted
	// (RuoYi: 默认部门,不允许删除).
	ErrDeptRootProtected = errors.BadRequest("DEPT_ROOT_PROTECTED", "the root department cannot be deleted")
	// ErrDeptParentInvalid: the parent is the department itself or one of its
	// descendants.
	ErrDeptParentInvalid = errors.BadRequest("DEPT_PARENT_INVALID", "parent cannot be the department itself or one of its descendants")
	// ErrDeptDisabled: the parent department is disabled and takes no
	// children (RuoYi: 部门停用，不允许新增).
	ErrDeptDisabled = errors.BadRequest("DEPT_DISABLED", "parent department is disabled")
	// ErrDeptInvalid: dept_name is blank.
	ErrDeptInvalid = errors.BadRequest("DEPT_INVALID", "dept_name is required")
)

var (
	// ErrPostNotFound: the requested post does not exist.
	ErrPostNotFound = errors.NotFound("POST_NOT_FOUND", "post not found")
	// ErrPostNameTaken: post_name already exists within the department.
	ErrPostNameTaken = errors.Conflict("POST_NAME_TAKEN", "post_name already exists within this department")
	// ErrPostCodeTaken: post_code already exists.
	ErrPostCodeTaken = errors.Conflict("POST_CODE_TAKEN", "post_code already exists")
	// ErrPostInvalid: post_name/post_code is blank or dept_id is missing.
	ErrPostInvalid = errors.BadRequest("POST_INVALID", "dept_id, post_name and post_code are required")
	// ErrPostInUse: the post still has System Users assigned and cannot be
	// deleted or disabled (RuoYi: 已分配，不能删除 / 该岗位下存在已分配用户，不能禁用).
	ErrPostInUse = errors.BadRequest("POST_IN_USE", "post is assigned to users and cannot be deleted or disabled")
	// ErrPostNotAssignable: one or more post ids in a Post Assignment do not
	// exist (or are soft-deleted/disabled).
	ErrPostNotAssignable = errors.BadRequest("POST_NOT_ASSIGNABLE", "one or more post ids do not exist or are disabled")
)

var (
	// ErrUserNotFound: the System User named by a Post Assignment operation
	// does not exist (or is soft-deleted). org owns this copy of the user
	// module's reason because the assignment endpoints are org's; the
	// client-visible contract is identical (the access precedent).
	ErrUserNotFound = errors.NotFound("USER_NOT_FOUND", "user not found")
)

var (
	// ErrOrgStoreUnavailable: an org-store operation behind the Dept, Post,
	// or Post Assignment repos failed unexpectedly — driver down, tx failure,
	// serialization. Adapters log the raw error once and return this sentinel
	// (ADR 0007), so no driver text crosses the repository boundary; clients
	// see 503, never a driver message.
	ErrOrgStoreUnavailable = errors.ServiceUnavailable("ORG_STORE_UNAVAILABLE", "org store unavailable")
)

// RootDeptID is the seeded root department — the organizational anchor the
// tree grows from and the fixed home for users that predate the org module.
// Withdrawal-protected (RuoYi's DEFAULT_DEPT_ID guard); the seed
// (docs/db/seed.sql) pins the id.
const RootDeptID int64 = 1000000000000000301
