# Implement the project-owned Kratos auth middleware

Type: task
Status: done 2026-08-16
Blocked by: 01, 04

## Objective

Enforce explicit proto policies using Kratos operations and publish an immutable
Principal without depending on the official Kratos integration.

## Work

- Implement a strict `Authorization: Bearer` parser; reject bare values and do
  not read cookie, query, or body credentials.
- Implement startup validation over all registered RPC descriptors. Missing or
  malformed auth policy must prevent serving.
- Resolve policy by `Transporter.Operation()`.
- For authenticated modes, preserve missing/malformed/expired/idle/kickout/
  replaced/storage-failure distinctions and explicitly check active timeout.
- Build Principal with System User ID, username, Session ID, Role keys, and
  Functional Permissions using a private typed context key and accessors.
- Enforce one permission handle for `PERMISSION`; support `superadmin` wildcard.
- Implement `OPTIONAL` behavior for Logout only. Invalid/expired is anonymous;
  a backend failure remains 503.
- Order middleware so recovery remains outermost and auth runs before request
  validation/business handlers.
- Wire it into the HTTP server only; keep the core transport-neutral.

## Acceptance

- Table-driven tests cover every policy mode and every 401/403/503 mapping.
- Public operations do not require Redis except when their own usecase does.
- Cookie/query/bare tokens never authenticate.
- Adding an unannotated RPC makes the completeness test/startup fail.
- No raw token appears in Principal, error text, or test logs.

## Comments

- 2026-08-16 added a transport-neutral `biz.Authenticator` and immutable
  `Principal` with private state, defensive-copy accessors, a private typed
  context key, exact Functional Permission checks, and the explicit
  `superadmin` `*:*:*` wildcard. Raw bearer credentials never enter Principal.
- Added the data authentication adapter over the injected sa-token Manager and
  authoritative Login Session index. It preserves invalid, expired, explicit
  idle-timeout, kickout, replacement, and backend-failure errors; validates the
  complete shared Authorization Snapshot; and refreshes activity only after a
  successful decode.
- Added startup descriptor enumeration and policy validation keyed by the exact
  generated `Transporter.Operation()`. Missing/unspecified/unknown policies,
  malformed permission handles, handles on non-permission modes, and OPTIONAL
  anywhere except Logout prevent HTTP server construction.
- Added strict, case-insensitive `Authorization: Bearer <non-empty-token>` HTTP
  parsing with no cookie/query/body/bare-value fallback. PUBLIC bypasses the
  authenticator; OPTIONAL treats only missing, confirmed-invalid, and expired
  credentials anonymously while backend failure remains 503.
- HTTP middleware is wired as recovery → authentication/authorization → request
  validation/handler. gRPC remains unenforced, and biz/data expose no HTTP
  types. Table-driven tests cover every policy mode, 401/403/503 mapping,
  strict credential parsing, public storage bypass, startup completeness,
  Principal immutability, snapshot corruption, and active/kickout/replacement
  state handling.
- Review hardening keeps a SHA-256 credential-fingerprint expiry tombstone for
  one additional fixed-lifetime window so natural TTL expiry remains distinct
  from an unknown token without retaining the credential. HTTP service
  registration and startup descriptor validation now consume the same
  registration list, and credential parser assertions never print raw values.
