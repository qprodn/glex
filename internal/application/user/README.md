# User Application

This package implements the System User service as an inbound adapter. It
parses protocol concerns such as DTO validation, AIP options, and field
masks, converts DTOs to domain inputs, and delegates to the user domain
service.

It may import API DTOs, `internal/domain/user/entity`, and
`internal/domain/user/service` (ADR 0005: the domain service is the module's
only repository holder). Same-package use cases own the transaction boundary,
compensation, and cross-domain contracts. It never imports
`internal/domain/user/repository` or Infrastructure.
