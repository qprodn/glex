# User Application

This package implements the System User service as an inbound adapter. It
parses protocol concerns such as DTO validation, AIP options, and field
masks, converts DTOs to domain inputs, and delegates to User Usecases.

It may import API DTOs and `internal/domain/user`. Transport entries only
adapt DTOs; same-package Usecases own transactions, compensation, and calls
to Domain ports. It never imports Infrastructure.
