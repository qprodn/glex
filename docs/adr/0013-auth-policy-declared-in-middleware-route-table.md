# Declare auth policy in a middleware route table, not proto metadata

Date: 2026-08-19

Status: Accepted

Supersedes: 0009-rpc-auth-policy-is-explicit-proto-metadata.md

## Context

ADR 0009 put every RPC's auth policy in a custom proto method option and made a
missing policy a startup error, backed by protoreflect validation machinery.
That machinery exists only to feed a kratos middleware that matches on the
operation name anyway. The project wants a simpler scheme modelled on
sa-token-go's kratos integration (route table checked by middleware), but
project-owned and without wildcard matching.

## Decision

- Delete `policy.proto` and every `auth_policy` method option; the public API
  contract carries no security metadata. No `make api` regeneration of policy
  stubs remains.
- Auth policy is declared in one centralized Go route table keyed by operation
  name (`/package.Service/Method`), living beside the middleware.
- The middleware lives in `pkg/middleware`, owns its minimal interfaces
  (authenticator, principal) and its own error variables; `internal/server`
  adapts `domain/iam` implementations into them. `pkg/` imports nothing from
  `internal/`.
- An operation absent from the table is **public**: no check, pass-through.
  This deliberately drops 0009's exhaustiveness guarantee — an unlisted new
  endpoint is silently public.
- The table is taken as-is: no startup validation of entries, handles, or
  coverage.
- A table entry may combine checks: a non-empty `perm` requires the Functional
  Permission handle, a non-empty `role` requires the role key, either implies
  login, and a login-only entry just requires authentication. No wildcards.
- Role checks are strict role-key matches; the superadmin permission wildcard
  (`*:*:*`) bypasses permission checks only, never role checks.
- The middleware always attempts authentication when a bearer token is present
  and injects the principal into the context, even for unlisted operations;
  enforcement of table checks happens after. This keeps logout (previously the
  only OPTIONAL route) working unchanged.
- gRPC remains without auth middleware (HTTP only), unchanged.

## Consequences

- Losing the fail-fast guarantee means new endpoints default to public; code
  review of the route table is the only guard.
- One file lists every protected operation — auditable at a glance.
- sa-token's optional-authentication behavior (principal available regardless
  of enforcement) comes for free; every public endpoint pays one session lookup
  when a token is present.
- Errors are defined by the middleware package; HTTP status codes and error
  reasons stay identical to today's, so clients see no change.
