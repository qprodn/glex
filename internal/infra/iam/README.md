# IAM Infrastructure

This package implements the ports declared by `internal/domain/iam` using Ent,
PostgreSQL, Redis, and sa-token. Persistent objects, driver errors, and query
translation remain inside this adapter package. Writes join the
context-propagated transaction opened by an Application Usecase.
