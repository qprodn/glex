# Implement login and the shared Authorization Snapshot

Type: task
Status: ready-for-agent
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
