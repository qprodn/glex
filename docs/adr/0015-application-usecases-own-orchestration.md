---
status: superseded by ADR-0020
---

# Application Usecases own orchestration

Superseded. The separate Usecase layer is gone; transport Application
services own the orchestration inline and domain services own the
module-local rules; see ADR-0020.

Application Usecases own business ordering, PostgreSQL transaction boundaries,
and explicit compensation while Domain owns DOs, rules, errors, and port
interfaces. This keeps generated Kratos entries as DTO adapters and makes the
transaction port belong to its Application consumer. Infra joins the context
transaction for writes and `cmd` binds its concrete transaction manager to the
Application port; Domain Operations and repository-owned transactions are not
used.

## Consequences

Usecases are transaction roots and must not nest. Repository writes rely on
their owning Usecase to enter a transaction, with this convention protected by
tests and review rather than a runtime guard. Concurrency constraints remain
best-effort: no database uniqueness constraints or PostgreSQL advisory locks
are introduced by this decision.
