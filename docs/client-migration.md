# Client migration: `api/iam` → `api/user` / `api/access` / `api/authn`

The public proto contracts moved off `api/iam` when the iam module split into
user, access, and authn (ADR 0016). The split is deliberately breaking for
generated clients; HTTP paths and JSON shapes are unchanged.

## New import paths

| Old                          | New               |
|------------------------------|-------------------|
| `glex/api/iam/user/v1`       | `glex/api/user/v1` |
| `glex/api/iam/role/v1`       | `glex/api/access/v1` |
| `glex/api/iam/menu/v1`       | `glex/api/access/v1` |
| `glex/api/iam/auth/v1`       | `glex/api/authn/v1` |

Proto packages follow the same move: `iam.user.v1` → `user.v1`,
`iam.role.v1`/`iam.menu.v1` → `access.v1`, `iam.auth.v1` → `authn.v1`. Role
and Menu now share one `access.v1` package, so their two `ErrorReason` enums
merged into a single enum; the wire-visible reason **names** (`ROLE_NOT_FOUND`,
`MENU_NOT_FOUND`, …) are unchanged — clients that branch on names need no
change. Generated TypeScript clients moved from `web/src/services/iam/<svc>/v1`
to `web/src/services/{user,access,authn}/v1`.

## Service grouping

- **user.v1.UserService** — unchanged.
- **access.v1.RoleService**, **access.v1.MenuService** — unchanged shapes, new
  package.
- **access.v1.RoleAssignmentService** — new standalone service hosting the
  three Role Assignment RPCs that previously lived on `iam.auth.v1.AuthService`:
  `ListUserRoles`, `UpdateUserRoles`, and `ListRoleUsers` (RuoYi's authRole
  dialog). Same request/reply messages, same HTTP paths (`GET/PUT
  /iam/user/{user_id}/roles`, `GET /iam/role/{role_id}/users`), same
  permission policy.
- **authn.v1.AuthService** — now only `Login` and `Logout`.
- **authn.v1.OnlineSessionService** — unchanged shape, new package.

## What does not change

- Every HTTP verb, path, query parameter, and JSON field name.
- The `reason` strings clients branch on for error handling.
- Permission policy per operation (the auth-policy route table follows the new
  operation identifiers, e.g. `/access.v1.RoleAssignmentService/ListUserRoles`,
  with identical checks).
