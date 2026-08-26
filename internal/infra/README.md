# Infrastructure

Infrastructure contains outward adapters:

- `store/` owns shared PostgreSQL, Redis, and transaction resources.
- `user/` implements the user module's ports (System User profiles).
- `access/` implements the access module's ports (Roles, Menus, Role
  Assignments, Authorization Resolution).
- `authn/` implements the authn module's ports (Credentials, Login Sessions,
  sa-token machinery).
- `ent/` contains Ent schema and generated persistence code.

Infrastructure may import its own module's Domain package to implement its
ports. It must not import another module's domain, application, or crossdomain
packages (ADR 0016), nor Application or Proto DTO packages.
