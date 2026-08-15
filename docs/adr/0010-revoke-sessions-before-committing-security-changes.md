# Revoke sessions before committing security changes

Login and every security-relevant write acquire the same transaction-scoped
PostgreSQL advisory lock for each affected System User; multi-user operations
lock sorted ids. While the database change is still uncommitted, the operation
atomically revokes the affected Login Sessions through the Redis `System User ->
Login Sessions` index, then commits PostgreSQL. A Redis failure rolls back the
database change, preventing stale authorization from surviving a committed
revocation. PostgreSQL cannot roll Redis back: if Redis succeeds but the database
commit later fails, users remain unnecessarily logged out. We accept that
fail-safe one-way inconsistency, and the longer database transactions it
requires, rather than accept an authorization-revocation window or introduce a
durable outbox. The protocol applies to Role Assignment and Menu Grant changes;
Role `role_key`, status, and deletion; Menu `perms`, status, and deletion; System
User status and deletion; and password changes. A user changing their own
password excludes the current Login Session from revocation; administrative
reset revokes all.
