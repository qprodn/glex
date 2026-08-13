# System User is a fresh module; the legacy admin resource is left in place

> **Superseded by [ADR-0004](./0004-remove-template-admin-and-auth.md)** — the
> admin resource and template auth were later removed entirely.

The System User (系统用户) module is written fresh on Kratos. It does not evolve,
merge with, or build on top of the pre-existing `admin` resource. `admin` is left
in place, and the new module neither depends on nor modifies it.

The repo already shipped a working `admin` resource (JWT-in-cookie auth, bcrypt,
ent/MySQL CRUD) — **not** the stock Kratos greeter scaffold. We reuse its
*infrastructure* (Kratos layering, ent, AIP, wire) but not its *domain model*: a
single `access` string with no RBAC and only ~40% of RuoYi's `sys_user` fields is
too thin and wrong-shaped to evolve into the system user. Admin and System User
are distinct concepts (see `CONTEXT.md`).

Whether `admin`'s auth is later unified with the System User module is deferred
to the auth module. Cost of this choice: two user-like resources coexist in the
repo until `admin` is removed.
