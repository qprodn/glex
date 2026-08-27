# Audit fields on every table; soft delete only

RuoYi's join tables (`sys_user_role`, `sys_role_menu`) carry only the two id
columns and hard-delete their rows on full replacement, and the repo's early
schema comments encoded that exception. We instead made it a blanket rule
(decided 2026-08-27): **every** PostgreSQL table — entity or join, meaningful
or not, including future append-only log tables — carries the five audit
fields (`create_by`, `update_by` bigint default 0; `created_at`,
`updated_at` timestamptz default now(); `deleted_at` timestamptz nullable),
and **no PostgreSQL row is ever hard-deleted**: withdrawal sets `deleted_at`.

`create_by`/`update_by` are stamped server-side from the authenticated
Principal in the Application layer; client DTOs never carry them, and 0 means
the system itself acted (bootstrap, migration). The rule is enforced
structurally by a shared ent audit mixin so a new schema cannot forget it.

## Considered Options

Join-table primary keys under soft delete (a re-granted pair would collide
with its withdrawn generation):

- **Chosen:** snowflake surrogate PK (`role_menu_id` / `user_role_id`) plus a
  partial unique index on the id pair `WHERE deleted_at IS NULL`. A re-grant
  inserts a new row; withdrawn generations accumulate as history.
- **Rejected:** keep the composite PK and resurrect the withdrawn row on
  re-grant (upsert clearing `deleted_at`). Reuses the row but makes
  withdrawal a reversible state and turns every grant write into an upsert.

## Consequences

- The join tables deviate from RuoYi's shape and are explicit ent entities,
  not generated M2M edge tables (ent edge tables cannot carry extra columns).
- Every join read filters `deleted_at IS NULL`; full-replacement writes
  soft-delete the active rows, then insert the new set with fresh snowflake
  ids and the actor stamped in `create_by`/`update_by`.
- Redis is outside the rule: a revoked Login Session's keys must physically
  disappear — a "soft-deleted" credential is a live credential.
- Withdrawn link generations are kept forever (no GC); back-office volume
  makes that acceptable.
