# Implement the authoritative Login Session store

Type: task
Status: ready-for-agent
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
