# Implement the project-owned Kratos auth middleware

Type: task
Status: ready-for-agent
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
