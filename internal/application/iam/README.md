# IAM Application

This package implements the generated IAM Kratos interfaces as inbound
adapters. It parses protocol concerns such as DTO validation, AIP options, and
field masks, converts DTOs to domain inputs, and delegates to IAM Usecases.

It may import API DTOs and `internal/domain/iam`. Transport entries only adapt
DTOs; same-package Usecases own transactions, compensation, and calls to
Domain ports. It never imports Infrastructure.
