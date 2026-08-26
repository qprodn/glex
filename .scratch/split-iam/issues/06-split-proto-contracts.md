# 06 — Split the public proto contracts

**What to build:** Split the external contracts off api/iam into
`api/user/v1`, `api/access/v1`, and `api/authn/v1`. access gets the Role and
Menu services plus a new standalone RoleAssignmentService hosting the three
assignment RPCs (ListUserRoles, UpdateUserRoles, ListRoleUsers) moved off the
auth service; authn gets login/logout plus the online-session service; user
keeps the user service unchanged. Regenerate the stubs; application adapters,
server registration, and the auth-policy route table follow the new operation
identifiers. Write the short client-migration note: new import paths, and the
assignment RPCs' new home.

**Blocked by:** 05 — rename remainder to user.

**Status:** done

- [x] api/iam is gone; the three proto trees exist with the agreed service grouping, including a standalone RoleAssignmentService
- [x] Application adapters, server registration, and the route table point at the regenerated contracts; middleware behavior unchanged
- [x] HTTP contract tests re-pinned to the new operations and green
- [x] Wire regenerated; `make all` builds on the branch

## Comments

Implemented on the `split-iam` integration branch as the contracts half of
ADR 0016's split (accepted breaking; consumers are our own frontends):

- **Trees** — `api/iam` deleted. `api/user/v1` (user.proto, shape unchanged),
  `api/authn/v1` (auth.proto with Login/Logout + Principal,
  online_session.proto), and `api/access/v1` (role.proto, menu.proto, and the
  new role_assignment.proto hosting `access.v1.RoleAssignmentService` —
  ListUserRoles/UpdateUserRoles/ListRoleUsers with the same messages, HTTP
  paths, and permission policy they had on AuthService). Every HTTP verb,
  path, and JSON field name is unchanged; only proto packages
  (`iam.*.v1` → `user/access/authn.v1`), import paths, and the assignment
  RPCs' service home moved.
- **Error reasons** — role's and menu's `ErrorReason` enums merged into one
  `access.v1.ErrorReason` (proto forbids two same-named enums in one package);
  menu values renumbered 7-11. Only the names cross the wire (protojson), so
  clients branching on reason names are unaffected. user/authn enums moved
  unchanged.
- **Regeneration** — `make api` (Go/gRPC/HTTP stubs, openapi.yaml,
  web/src/services TS clients: `iam/<svc>/v1` → `{user,access,authn}/v1`) and
  `make all`; `wire_gen.go` is byte-identical (no provider graph or
  constructor signature changed — the removed `authServiceServer` join struct
  only lived inside http.go).
- **Consumers** — application adapters alias `accessv1`/`authnv1`/`userv1`;
  `RoleAssignmentApplication` embeds `UnimplementedRoleAssignmentServiceServer`;
  server registers the six HTTP services directly (join struct gone; gRPC
  still registers user/menu/role only, as before); the ADR 0013 route table
  re-keys to the new operation identifiers with every permission string and
  Login flag preserved one-for-one.
- **Tests** — the former single auth-tree contract test was re-homed per
  module: each tree pins its own confirmed routes (`authn.v1.*` including the
  no-unexpected-public-route guard, `access.v1.*` including GetRouters static
  precedence and the three assignment paths, `user.v1.*` GetInfo) and its own
  no-secret DTO checks; descriptor helpers are deliberately local so a future
  service extraction takes its contract test along unchanged. logging/
  error_reason tests moved with their trees.
- **Note** — `docs/client-migration.md`: new import paths (Go + TS + proto
  packages), the assignment RPCs' new home, and what does not change.

Code-review findings: zero hard standards violations and no spec gaps; the
flagged comment rewording in protos ("the auth module" references that no
longer exist post-split) and the merged enum were judged necessary fallout,
not scope creep. `make all`, `go vet`, and the full test suite are green on
the branch (env-gated real-store integration runs belong to ticket 07).
