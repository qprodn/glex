# IAM Application

This package implements the generated IAM Kratos interfaces and owns IAM
operation orchestration: DTO ↔ domain conversion, transactions, authorization
checks, compensation, and coordination between IAM ports.

It may import API DTOs and `internal/domain/iam`. It must never import
`internal/infra`, Ent, Redis, or concrete adapters.
