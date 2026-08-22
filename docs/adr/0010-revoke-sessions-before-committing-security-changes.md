# Revoke sessions before committing security changes

> Superseded by [0012](0012-context-propagated-transactions.md) as the
> mechanism (coordinator → ctx-propagated InTx with the revocation moved into
> the Application operations). The ordering semantics below are unchanged.

Login and every security-relevant write acquire the same transaction-scoped
PostgreSQL advisory lock for each affected System User; multi-user operations
lock sorted ids. Locks are namespaced and acquired in one global order:
**Menu IDs, then Role IDs, then System User IDs**, ascending inside each type.
Before concrete identities, Menu Grant writers take a per-Role grant seam in
the Menu phase and Role Assignment writers take a per-User assignment seam in
the Role phase. Those seams stabilize the old relationship set; writers then
lock both old and replacement identities and re-read the relationship under
the concrete locks. Downstream affected users are re-read after Role locks are
held. Login takes the same namespaced System User lock.
While the database change is still uncommitted, the operation
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
