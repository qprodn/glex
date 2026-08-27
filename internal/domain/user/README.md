# User Domain

This module owns the System User domain object, its security-change policy,
typed errors, and its repository port. The Application service owns business
ordering, transactions, compensation, and coordination across these ports.

Layout (coze-style split):

- `errors.go` (package root) — the module's typed errors.
- `entity/` — the User domain object, CurrentUserInfo, and the
  SessionRevocation policy value objects.
- `repository/` — the UserRepo port, its shapes, and the ListOption family.
- `service/` — the module's own rules (uniqueness assertion) that need no
  other module and no transaction boundary.

The module must not import Application, Infrastructure, Proto DTOs, Ent,
Redis, or storage-specific types, and never another domain module.
