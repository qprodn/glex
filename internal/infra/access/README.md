# Access Infrastructure

This package implements the ports declared by `internal/domain/access` —
Roles, Menus, Role Assignments, and Authorization Resolution — using Ent and
PostgreSQL. Persistent objects, driver errors, and query translation remain
inside this adapter package. Writes join the context-propagated transaction
opened by an Application Usecase.
