# Expose login, logout, and RuoYi online-session operations

Type: task
Status: ready-for-agent
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
