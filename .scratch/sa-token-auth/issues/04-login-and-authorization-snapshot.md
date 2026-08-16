# Implement login and the shared Authorization Snapshot

Type: task
Status: done 2026-08-16
Blocked by: 03

## Objective

Complete username/password login and rebuild the account-level Role/permission
snapshot safely after every sa-token login.

## Work

- Extend `biz.AuthUsecase` and its repo interfaces for credential lookup,
  bcrypt verification, account-state validation, login counters, active Role
  keys, Functional Permission resolution, and last-login update.
- Return the same `INVALID_CREDENTIALS` error for an unknown username and a bad
  password; only reveal `ACCOUNT_DISABLED` after successful password checking.
- Enforce username and authoritative-source-IP counters with the configured
  thresholds, TTLs, and remaining lock duration.
- Implement the PostgreSQL advisory-lock entry needed to serialize login with
  security-changing mutations.
- Call `Manager.Login` only from the data wrapper, then rebuild the shared
  Account Session in one logical snapshot update before exposing the token.
- Add `*:*:*` when an active assigned Role has `role_key=superadmin`.
- Register the new Session in the authoritative project index and evict the
  oldest when necessary.
- Update last-login IP/time after successful login without adding an audit table.
- Derive device/browser/OS metadata from User-Agent.

## Acceptance

- Unknown user and bad password are externally indistinguishable.
- Counters lock and expire correctly; successful login clears the username
  counter.
- Disabled accounts cannot create Sessions.
- A second login clears then rebuilds the Account Session before returning and
  both tokens subsequently see the same Roles/permissions.
- `superadmin` receives the wildcard only through the Role.
- No call site outside the data wrapper invokes `Manager.Login`.

## Comments

- 2026-08-16 implemented `AuthUsecase.Login` with bcrypt verification, a
  constant-work dummy hash for unknown System Users, post-password account
  status validation, and stable `INVALID_CREDENTIALS`, `ACCOUNT_DISABLED`, and
  duration-bearing `LOGIN_LOCKED` errors.
- Added Lua-atomic Redis failure counters for username (5/10m) and authoritative
  source IP (20/10m). Subjects are SHA-256-fingerprinted in Redis keys; lock TTL
  is reported without revealing which subject caused it, and success clears
  only the username counter.
- Added the sole data-layer `Manager.Login` wrapper. It takes the per-System
  User PostgreSQL transaction advisory lock, performs a post-lock authoritative
  credential/status re-read, resolves active Role keys and distinct active
  Functional Permissions, then restores the shared Account Session in one
  `SetMulti` write before registering the project Login Session or returning the
  bearer credential.
- The wrapper grants `*:*:*` only when the active Role Assignment includes
  `role_key=superadmin`, uses the authoritative project index for oldest-Session
  eviction, updates last-login IP/time in the protected PostgreSQL transaction,
  and cleans up an unreturned Login Session when the database transaction fails.
- User-Agent parsing derives safe desktop/mobile/tablet/bot, browser, and OS
  metadata. Tests cover indistinguishable unknown-user/bad-password behavior,
  disabled accounts, both counters and expiry, post-lock credential recheck,
  advisory-lock SQL, snapshot replacement across two tokens, wildcard rules,
  safe Session registration, and last-login persistence.
- Review hardening filters a Menu-held `*:*:*` from non-superadmin Roles,
  stores Snowflake System User IDs as exact decimal strings in the Account
  Session, maps database driver failures to the typed auth-backend error, and
  refuses startup unless the exclusive edge-proxy source-IP topology is
  explicitly asserted.
