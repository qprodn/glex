# Single-tenant system — no tenant_id

glex is a **single-tenant** system. No table carries a `tenant_id` column and no
tenant-scoping filter is applied to queries. Decided 2026-08-12 at System User
MVP planning; intended to hold repo-wide.

RuoYi-Vue-Plus ships multi-tenancy, but in the checkout we are porting from, the
`TenantLineInnerInterceptor` appears only in a comment and is never wired — there
is no tenant column to preserve. We choose single-tenant because the target is an
internal admin system, not a multi-org product.

Hard to reverse: adding tenancy later means a column + global query filter + data
backfill on every table. If the system ever needs to serve multiple organizations,
treat this as superseded and budget a full migration.
