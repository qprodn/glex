# sa-token authentication and authorization

Status: confirmed

## Problem

Add production-grade authentication, Login Session management, and RuoYi-style
Functional Permission checks to the single-tenant glex Kratos service. Reuse
`sa-token-go` core token/session mechanics, but own the Kratos adapter and keep
DTO, domain, persistence, and transport concerns inside the repository's
existing layer boundaries.

## Scope

The first release provides:

- username/password login and idempotent logout;
- revocable multi-device Login Sessions backed by Redis;
- a shared account-level Authorization Snapshot of Role keys and Functional
  Permissions;
- explicit per-RPC authentication policy and one-permission authorization;
- current-user information and the current user's route tree;
- the same online-session operations exposed by RuoYi-Vue-Plus;
- immediate, fail-safe Session revocation for authorization-bearing writes;
- HTTP middleware, with a transport-neutral core that can support gRPC later.

The first release does not provide:

- data-scope authorization, departments, or SQL rewriting;
- captcha, SMS, email, social, mini-program, client, or grant-type login;
- a login-audit resource or table;
- stateless JWT;
- compound AND/OR/role policy expressions;
- new public bulk logout operations;
- gRPC server enforcement in this release.

## Domain language

Use the terms defined in `CONTEXT.md`: System User, Role, Menu Grant, Role
Assignment, Functional Permission, Login Session, Session ID, and Authorization
Snapshot. In particular, a Session ID is a non-secret management handle; it is
never a bearer credential.

## External HTTP surface

Use the behavior and routes already present in RuoYi-Vue-Plus. Proto messages,
errors, field naming, and list wrappers follow glex conventions rather than
copying Java `R<PageResult>` wrappers.

| Method | Path | Policy | Semantics |
|---|---|---|---|
| `POST` | `/auth/login` | `PUBLIC` | Create one Login Session from username/password. |
| `POST` | `/auth/logout` | `OPTIONAL` | Revoke the presented valid Session; missing or confirmed-invalid credentials succeed. |
| `GET` | `/monitor/online/list` | `monitor:online:list` | List all active Login Sessions. |
| `DELETE` | `/monitor/online/{tokenId}` | `monitor:online:forceLogout` | Administrator revokes one Session ID. |
| `GET` | `/monitor/online` | `AUTHENTICATED` | List the current System User's Login Sessions. |
| `DELETE` | `/monitor/online/myself/{tokenId}` | `AUTHENTICATED` | Revoke one Session only after proving it belongs to the current System User. |
| `GET` | `/system/user/getInfo` | `AUTHENTICATED` | Return the current System User, Role keys, and Functional Permissions. |
| `GET` | `/system/menu/getRouters` | `AUTHENTICATED` | Return the current System User's current route tree. |

Do not add public "revoke every Session", "revoke all Sessions for a user", or
"log out all users" operations. Authorization-change revocation is an internal
business invariant, not an HTTP management endpoint.

### DTO shapes

Login request:

- `username`
- `password`

Login reply:

- `access_token`
- `expire_in`

Online Session rows:

- `token_id` serialized as `tokenId`, containing the non-secret Session ID;
- `user_name`;
- `device_type`;
- `ipaddr`;
- `browser`;
- `os`;
- `login_time`.

Derive device type, browser, and OS from User-Agent. Do not add `deviceName`,
`clientId`, `grantType`, `deptName`, `clientKey`, or `loginLocation` merely to
fill fields from the RuoYi response.

## Dependency and token contract

- Pin `sa-token-go` to v0.2.4.
- Import core only; do not import `integrations/kratos` or use the process-global
  `stputil` facade.
- Construct and inject `*core.Manager` through Wire behind data-layer contracts.
- Explicitly validate sa-token configuration before constructing the Manager.
- Generate opaque random credentials with `core.TokenStyleSimple`.
- Accept only `Authorization: Bearer <token>` with a strict, case-insensitive
  Bearer scheme and a non-empty token.
- Do not fall back to cookie, query string, request body, or a bare header value.
- Never log, return, or put the raw bearer token into Principal.

## Login behavior

1. Resolve the System User by username.
2. Return `INVALID_CREDENTIALS` for both an unknown username and a bad password.
3. Verify the stored bcrypt hash.
4. After successful password verification, reject a disabled System User with
   `ACCOUNT_DISABLED`.
5. Enforce two Redis-backed counters:
   - username: five failures in ten minutes;
   - source IP: twenty failures in ten minutes.
6. A locked attempt returns `LOGIN_LOCKED` with remaining duration. A successful
   login clears the username counter.
7. Obtain the authorization concurrency locks, load the active Role keys and
   Functional Permissions, and create the Login Session.
8. Update the System User's last login IP and time. Do not add a login-audit
   table in this release.

The application trusts the forwarded source-IP header only because deployment
must make the service port unreachable and the exclusive edge proxy must remove
the inbound forwarding header before writing the authoritative value. A deploy
check must fail or be blocked when this topology is not true.

## Login Session model

- Allow concurrent Login Sessions.
- Every login creates a distinct bearer token (`IsShare=false`).
- Allow at most five Login Sessions per System User.
- Registration of a sixth Session atomically marks/revokes the oldest Session
  and preserves a distinct 401 reason for the displaced client.
- Fixed lifetime: seven days.
- Idle lifetime: thirty minutes; middleware must explicitly execute the active
  timeout check because normal sa-token login checks do not.
- Logout affects only the current Login Session.

### Authoritative project index

Do not rely on sa-token's account/device mapping, token enumeration, or
`MaxLoginCount`: v0.2.4 can overwrite a device mapping while leaving an older
token valid. Maintain project-owned Redis structures, updated atomically via
Lua, for:

- System User -> ordered Login Sessions;
- Session ID -> token/session metadata;
- token fingerprint -> Session ID, where required internally;
- ownership checks;
- oldest-Session eviction;
- targeted and all-user internal revocation;
- last-Session cleanup.

The public Session ID must not permit derivation of the token. Redis keys use a
glex auth namespace and must not expose a storage-wide destructive `Clear` in
application flows.

## Authorization Snapshot

All of a System User's Login Sessions share one sa-token Account Session whose
data contains:

- System User ID and username;
- active Role keys;
- distinct Functional Permission handles;
- the `superadmin` wildcard when an active `role_key=superadmin` is assigned.

Do not query PostgreSQL on every permission check. Do not store the route tree in
the Authorization Snapshot.

`sa-token-go` v0.2.4 `Manager.Login` rewrites the whole Account Session from an
empty value when it issues a new token. Therefore:

- no caller outside the data wrapper may call `Manager.Login`;
- login must be serialized against other login/security mutations for that
  identity;
- immediately after `Manager.Login`, write Roles and Functional Permissions
  back to the Account Session in one logical snapshot update;
- do not return the token until the snapshot exists;
- accept that already-running requests can briefly observe an empty snapshot and
  receive a false 403; the interval must never grant permission;
- when the project index reaches zero Login Sessions, explicitly delete the
  lingering Account Session.

## Endpoint policy

Define a shared proto method option with these modes:

- `PUBLIC`;
- `OPTIONAL`;
- `AUTHENTICATED`;
- `PERMISSION`, carrying exactly one Functional Permission handle.

`UNSPECIFIED` is invalid. At startup, enumerate every registered RPC operation
and fail before serving when a policy is absent or malformed. `PERMISSION`
requires a non-empty handle; the other modes reject a handle. The first version
does not implement compound expressions.

Middleware uses Kratos `Transporter.Operation()` as the stable policy key, never
an HTTP path. It produces an immutable Principal containing System User ID,
username, Session ID, Role keys, and Functional Permissions. Use a private typed
context key and public accessors.

`OPTIONAL` exists for Logout only:

- missing credential: continue anonymously and succeed;
- confirmed-invalid/expired credential: continue anonymously and succeed;
- valid credential: attach Principal and revoke its Session;
- Redis/storage failure: return 503 rather than claim logout succeeded.

## Routes and current-user data

`GET /system/user/getInfo` reads the System User record plus Role keys and
Functional Permissions from the authenticated identity/snapshot. Never expose
the password hash.

`GET /system/menu/getRouters` queries current active Role Assignments, Menu
Grants, and active Menus each time. It returns Directory/Menu nodes, excludes
Button nodes, uses the existing menu-tree rules, and grants all active route
nodes to `superadmin`. It is presentation, not the authorization decision.

## Security-change revocation protocol

An Authorization Snapshot becomes invalid when any of these changes:

- Role Assignment;
- Menu Grant;
- Role `role_key`, status, or deletion;
- Menu `perms`, status, or deletion;
- System User status or deletion;
- password modification or reset.

A user changing their own password retains the current Login Session and loses
the others. An administrator resetting a password revokes all Login Sessions.

PostgreSQL cannot atomically roll Redis back. Use this fail-safe protocol:

1. Start a PostgreSQL transaction.
2. Determine affected Menu/Role/System User identities and acquire
   transaction-scoped advisory locks in a single documented global order. At a
   minimum, every affected System User shares the same lock with login.
3. Re-read the protected relationships after locks are held when the initial
   lookup could race with assignment/grant changes.
4. Apply the database mutation without committing.
5. Atomically revoke the affected Redis Login Sessions.
6. If Redis fails, roll back PostgreSQL and return an error.
7. If Redis succeeds, commit PostgreSQL.

If Redis succeeds but PostgreSQL commit fails, do not attempt to restore the
Sessions; unnecessary logout is the accepted safe-side outcome. Do not use an
outbox. Tests must prove that concurrent login, Role Assignment, Menu Grant, and
Menu permission changes cannot leave a committed stale snapshot.

## Error contract

Login reasons:

- `INVALID_CREDENTIALS`;
- `ACCOUNT_DISABLED`;
- `LOGIN_LOCKED`.

Authentication failures return 401 with stable distinct reasons for:

- missing credential;
- malformed Bearer credential;
- invalid or fixed-expired token;
- idle timeout;
- administrator kickout;
- replacement/Session-limit eviction.

Authorization denial returns 403. Redis, account-Session decoding, or other
authentication backend failures return 503 and must not be folded into 401/403.

## Layer ownership

- `api/system/auth/v1`: auth/session DTOs, error reasons, and policy option;
  user/menu protos receive `getInfo`/`getRouters` operations.
- `internal/service`: DTO <-> DO only; no token parsing or Redis access.
- `internal/biz`: Principal and auth/session DOs, `AuthUsecase`, login and
  authorization rules, and repo interfaces.
- `internal/data`: sa-token Manager wrapper, custom Redis `core.Storage`, project
  Session index, permission resolution queries, advisory-lock transaction
  implementation, and driver-error mapping.
- `internal/server`: custom Kratos policy/authentication middleware and service
  registration. It calls biz capabilities and never constructs storage clients.
- `cmd`: Wire composition only.

`Data` owns the shared Redis client. Generated proto/Wire/ent files are produced
by repository generators and are never edited manually.

## Verification

Required unit coverage:

- strict Bearer parser and rejection of cookie/query/bare tokens;
- policy validation and startup completeness;
- Principal context access;
- password and account-state behavior;
- dual login counters;
- Role/permission snapshot and `superadmin` wildcard;
- 401/403/503 mapping;
- Logout optional-auth semantics;
- Session ownership checks and safe Session IDs;
- router filtering and tree construction.

Required real PostgreSQL/Redis integration coverage:

- two service instances sharing Session state;
- distinct concurrent tokens and a shared snapshot;
- five-Session oldest eviction;
- fixed and idle expiry;
- last-Session Account Session cleanup;
- login rebuilding the Account Session after `Manager.Login` clears it;
- every Role/Menu/User/password revocation trigger;
- self-password change preserving only the current Session;
- Redis failure rolling back PostgreSQL;
- Redis success followed by PostgreSQL commit failure leaving only unnecessary
  logout, never stale authorization;
- concurrent login/assignment/grant/menu-change races;
- Redis partial-operation behavior and Lua atomicity.

Run the whole Go suite and `go test -race`. Generation must be clean under
`make api`/`make all`, and tests must not require real credentials.

## Decisions

- `docs/adr/0007-sa-token-core-with-project-owned-kratos-adapter.md`
- `docs/adr/0008-login-sessions-share-an-account-authorization-snapshot.md`
- `docs/adr/0009-rpc-auth-policy-is-explicit-proto-metadata.md`
- `docs/adr/0010-revoke-sessions-before-committing-security-changes.md`
- `docs/adr/0011-source-ip-is-set-by-an-exclusive-edge-proxy.md`
