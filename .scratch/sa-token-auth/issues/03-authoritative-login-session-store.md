# Implement the authoritative Login Session store

Type: task
Status: done 2026-08-15
Blocked by: 02

## Objective

Implement project-owned Session IDs and Redis indexes so multi-token behavior
does not depend on sa-token's lossy loginID/device mapping.

## Work

- Add biz DOs and repo interfaces for Login Session creation, lookup, listing,
  ownership, logout, kickout, replacement, and all-user internal revocation.
- Generate a random, non-secret Session ID separate from the bearer token.
- Implement Redis structures for ordered `System User -> Login Sessions`,
  Session metadata, and the internal token/Session correlation needed for
  middleware.
- Use Lua for atomic registration, five-Session limit enforcement, oldest
  eviction, targeted revocation, ownership-checked self-removal, and last-Session
  cleanup/refcount decisions.
- Preserve specific states for administrator kickout versus oldest-Session
  replacement so middleware can return distinct 401 reasons.
- Explicitly delete the sa-token Account Session when the authoritative index
  reaches zero.
- Never list or return raw bearer tokens.

## Acceptance

- Six concurrent registrations leave exactly five usable Sessions and mark the
  oldest as replaced.
- Multiple logins reporting the same derived device type remain enumerable.
- Removing an old Session cannot orphan or hide a newer token.
- Self-removal cannot revoke another System User's Session ID.
- Last-Session cleanup deletes the shared Account Session.
- Real-Redis concurrency tests and `go test -race` pass.

## Comments

- 2026-08-15 implemented `biz.LoginSession` and the authoritative
  `LoginSessionRepo`: create, Session-ID/token lookup, global and per-System
  User listing, ownership checks, logout, administrator kickout, replacement,
  and internal all-user revocation with an optional preserved Session.
- Redis owns an ordered per-user index, a global listing index, safe metadata,
  and SHA-256 token-fingerprint correlation. Session IDs are random UUIDv4
  management handles; every public read converts through the DO and cannot
  return the stored bearer token.
- Registration, five-Session oldest eviction, targeted/ownership-checked
  revocation, and all-user revocation are Lua-atomic. Account/device mappings
  are deleted only when they still point at the revoked token, and the shared
  sa-token Account Session is explicitly deleted when the cleaned authoritative
  index reaches zero.
- System User IDs are serialized into Lua metadata as decimal strings, not JSON
  numbers, so Snowflake IDs above IEEE-754's exact range retain exact ownership
  and cleanup behavior. A regression test covers the first integer above
  `2^53`.
- The concurrency contract runs against miniredis by default and against a real
  Redis when `GLEX_REAL_REDIS_ADDR` is set. The real-Redis six-registration
  test was run locally and left exactly five usable Sessions plus one distinct
  replacement state. `make all`, `go test ./...`, and `go test -race ./...`
  passed.
