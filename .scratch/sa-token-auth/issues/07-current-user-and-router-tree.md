# Implement current-user information and the route tree

Type: task
Status: ready-for-agent
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
