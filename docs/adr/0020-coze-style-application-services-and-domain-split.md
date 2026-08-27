---
status: accepted
---

# Coze-style application services and entity/repository/service domain split

Adopt the coze-studio code organization (ADR 0017's source of inspiration):
merge the per-module Usecases into the transport Application services, and
split each domain module into `entity`, `repository`, and `service`
subpackages. Application services own the **cross-domain** orchestration —
DTO conversion, PostgreSQL transaction boundaries, compensation, and calls
through crossdomain contracts — directly in their RPC method bodies. Rules
that need no other module and no transaction boundary (uniqueness checks,
menu-grant validity, tree assembly, route selection, credential hashing,
login-failure normalization) move into `domain/<module>/service` as
stateless functions taking the module's ports as parameters. This supersedes
ADR-0015's separate Usecase layer; ADR-0016's per-module `Transaction` ports
stay, now consumed by the Application services themselves.

## Ruling

- Application: one service per proto service (`<Resource>Application`), whose
  struct holds the module's repository ports, the crossdomain contracts it
  consumes, and the `Transaction` port. RPC methods convert DTOs and
  orchestrate inline; private helper methods (e.g. `authenticate`,
  `writeCredential`) may carry the flow when the transport wrapper and the
  orchestration read better apart. No separate Usecase structs, constructors,
  or local usecase interfaces remain.
- Domain: the package root holds only the typed errors; `entity/` holds DOs
  and value objects (with their behavior), `repository/` holds ports, their
  request/response shapes, and the `ListOption` family, `service/` holds the
  module's own rules and intra-domain orchestration as functions over the
  ports. Domain never imports Application, Infra, another domain, or
  crossdomain; the domain service depends on its own module's repository
  package only.
- Wire and constructor injection are unchanged: `cmd` stays the only
  composition root, the three per-module `Transaction` ports stay bound to
  the one `*store.TxManager`, and crossdomain contracts (ADR 0017) stay
  constructor-injected.

## Consequences

Each Application service is its own transaction root; nesting is still not
supported and commit-failure compensation stays explicit at the service.
Domain service functions are pure with respect to transactions — they read
through ports and return typed errors, and the Application service decides
what runs inside `InTx`. The fake-Usecase test seam disappears; application
tests drive the real services through proto DTOs with fake ports, and
domain-level rule tests (tree assembly, route selection) target the domain
service directly. The mechanical `entity`/`repository`/`service` split that
ADR-era guidance previously forbade is now the required shape.
