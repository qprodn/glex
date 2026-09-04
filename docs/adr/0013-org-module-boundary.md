# Dept and Post live in a new org module

Departments (`sys_dept`), Posts (`sys_post`), and the user↔post link
(`sys_user_post`) are ported into a sixth module, **org**, with the standard
four layers (`api/org/v1` → `application/org` → `domain/org` → `infra/org`).
The user module keeps ownership of `sys_user` and gains only a nullable
`dept_id` column. Cross-module needs go through two single-interface crossdomain
contracts, named by provider like the existing three: `crossdomain/org` (org
provides; the user application validates a user's Department on save) and
`crossdomain/user` (user provides; the org application asks whether a
Department is still referenced by users, for its withdraw and disable guards)
(decided 2026-09-04).

## Considered Options

- **Chosen:** new org module. Follows the codebase precedent that a link table
  lives with the module owning the aggregate it links to (user↔role links live
  in access, not user), keeps CONTEXT.md's user module narrow — "the operator's
  profile" — since organization structure is not profile, and leaves a clean
  seam for the deferred data-permission port (access ↔ dept).
- **Rejected:** fold into the user module. Zero contracts and a single
  transaction across user + post writes, but it grows user into a five-concept
  module (user, dept, post, two links) and buries org structure inside
  identity.

## Consequences

- Two new crossdomain contract packages with one interface each, mockgen
  doubles and fx wiring like the existing three.
- Department names and the department tree are resolved in the web client (the
  tree is loaded for the page anyway); no API joins across the module boundary.
- `sys_user_post` follows the join-table doctrine: snowflake surrogate PK,
  partial unique index on (user_id, post_id) `WHERE deleted_at IS NULL`,
  full-replacement semantics, re-granted pair = new row.
