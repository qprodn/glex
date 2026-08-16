# Implement current-user information and the route tree

Type: task
Status: ready-for-human
Blocked by: 04, 05

## Objective

Complete the frontend auth bootstrap using the two RuoYi endpoints that were
previously deferred until identity and permission resolution existed.

## Work

- Implement `GET /system/user/getInfo` in the existing user service boundary.
- Return the current System User plus Role keys and Functional Permissions from
  Principal/Authorization Snapshot; omit password.
- Add repo/usecase queries for the current user's active Role Assignments, Menu
  Grants, and Menus.
- Implement `GET /system/menu/getRouters` in the existing menu service boundary.
- Build routes fresh on every call: include Directory/Menu nodes, exclude Button
  nodes, apply active/visibility and existing tree ordering rules, and allow
  `superadmin` all active routes.
- Do not derive routes by reverse matching `perms` and do not store route DTOs in
  the Account Session.

## Acceptance

- User info never returns the password hash.
- Ordinary users see only currently granted active routes; Button-only grants do
  not produce routes.
- Directories without `perms` still appear when granted/needed as ancestors.
- `superadmin` sees all active routeable nodes.
- Tree behavior covers missing/soft-deleted parents consistently with existing
  menu rules.

## Comments

- 2026-08-16 implemented `GET /system/user/getInfo`: the service requires the
  authenticated Principal, freshly loads the current System User through biz,
  and returns Role keys/Functional Permissions from the immutable
  Authorization Snapshot. The dedicated `CurrentUser` projection never carries
  the stored bcrypt hash.
- Implemented `GET /system/menu/getRouters` over fresh active Role Assignment,
  Menu Grant, and active Menu queries on every call. Ordinary users receive
  granted Directory/Menu nodes plus required active ancestors; Button-only and
  inactive grants do not produce routes. An exact active `superadmin` Role sees
  all active routeable nodes.
- Route construction reuses the existing sibling ordering and orphan behavior,
  preserves the repository's id tie-break for equal `order_num`, and implements
  RuoYi-compatible root Directory, root Menu frame, nested `ParentView`, and
  inner-link path/component metadata semantics. Static bootstrap RPCs are
  declared before parameter routes so generated HTTP registration cannot route
  `getInfo`/`getRouters` as numeric ids.
- Tests cover password non-disclosure, Principal/snapshot handoff, fresh route
  resolution, ancestor inclusion, Button/inactive exclusion, superadmin,
  missing parents, stable ordering, RuoYi route projection, database failure
  mapping, authentication policy, and real generated HTTP route precedence.
  Production server/Wire registration remains deferred to issue 09.
- `make all`, `go test ./...`, and `go test -race ./...` passed. The first
  two-axis review found four Standards and two Spec issues; fixes moved DTO
  projection into handlers, isolated layer tests, mapped route-query driver
  failures to `AUTH_BACKEND_UNAVAILABLE`, completed RuoYi projection behavior,
  and restored deterministic equal-order routing before final re-review.
- Final Standards and Spec re-reviews both completed with zero findings and zero
  blocking/non-blocking issues; the complete generated/tested baseline diff is
  ready for human review.
