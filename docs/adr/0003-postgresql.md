# PostgreSQL, not MySQL

glex runs on **PostgreSQL**, not MySQL. The Kratos template shipped
MySQL-configured (`driver: mysql`, `go-sql-driver/mysql`); we switch the whole
app to Postgres.

Decided 2026-08-13 at schema planning. Dev DB is local:
`postgres://postgres:123456@127.0.0.1:5432/glex`. ent is driver-agnostic, so the
swap is config + driver (pgx) + DSN; the legacy `admin` resource moves to
Postgres with it.

Consequences: RuoYi's `sys_user` DDL is the structure reference, but types are
Postgres-native — `char(1)` enums become `smallint`, timestamps are `timestamptz`,
soft-delete is `deleted_at timestamptz` (not `del_flag`). No MySQL-only types or
syntax.
