# IAM Domain

This package owns IAM domain objects, pure security policies, typed errors,
repository ports, and authentication capability ports. Application Usecases
own business ordering, transactions, compensation, and coordination across
these ports.

It must not import Application, Infrastructure, Proto DTOs, Ent, Redis, or
storage-specific types. Files are grouped by IAM concept; do not create generic
`bo`, `vo`, `model`, or `repository` subpackages.
