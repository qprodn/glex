# Shared Store

This package owns the process-wide PostgreSQL and Redis clients and implements
transaction propagation. Domain-specific queries and authentication mechanics
belong in sibling Infrastructure adapter packages such as `infra/iam`.
