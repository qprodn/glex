# Expose login, logout, and RuoYi online-session operations

Type: task
Status: ready-for-human
Blocked by: 03, 04, 05

## Objective

Implement the confirmed transport handlers without inventing additional public
Session-management operations.

## Work

- Implement `POST /auth/login` and map the login DO to
  `access_token/expire_in`.
- Implement `POST /auth/logout` under `OPTIONAL`: no Principal is success, a
  valid Principal revokes its Session, and Redis failure returns 503.
- Implement global online listing protected by `monitor:online:list`.
- Implement administrator single-Session kickout protected by
  `monitor:online:forceLogout`.
- Implement current-user Session listing and ownership-checked single-Session
  removal.
- Return only Session ID, username, derived device/browser/OS, IP, and login
  time. Do not expose bearer tokens or placeholder department/client/location
  data.
- Preserve idempotence for missing or already-invalid Logout credentials and
  already-absent kickout targets where the spec permits it.

## Acceptance

- HTTP route tests cover all six RuoYi operations and their policies.
- An administrator cannot reuse list output as a bearer credential.
- A System User cannot remove another user's Session ID.
- Logout with Redis unavailable returns 503 and does not claim success.
- No bulk public logout route is registered.

## Comments

- 2026-08-16 implemented the login transport boundary, including authoritative
  forwarded source IP and User-Agent metadata, and mapped the login DO to the
  `access_token` / `expire_in` reply without retaining or publishing the bearer
  credential elsewhere.
- Added the Login Session usecase and service operations for OPTIONAL
  current-Session logout, permission-protected global listing and targeted
  administrator kickout, authenticated current-user listing, and atomic
  ownership-checked current-user removal. Missing Logout credentials and absent
  revocation targets remain idempotent; storage failures propagate as 503.
- Online Session DTOs contain only the non-secret Session ID, username, derived
  device/browser/OS, IP address, and login time. No bulk logout RPC or HTTP
  route was added.
- Runtime HTTP tests register the generated handlers in isolation and exercise
  all six RuoYi routes and policy modes, list-output credential replay
  rejection, foreign Session ownership rejection, backend-failure handling,
  and the exact absence of any bulk auth/session route. Production ProviderSet
  and server registration remain scoped to issue 09; no unprotected gRPC
  session-management service is exposed.
- Verification completed with `make all`, `go test ./...`, and
  `go test -race ./...`.
