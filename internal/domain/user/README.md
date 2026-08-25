# User Domain

This package owns the System User domain object, its security-change policy,
typed errors, and the repository ports. Application Usecases own business
ordering, transactions, compensation, and coordination across these ports.

It must not import Application, Infrastructure, Proto DTOs, Ent, Redis, or
storage-specific types. Files are grouped by user-module concept; do not
create generic `bo`, `vo`, `model`, or `repository` subpackages.
