# Infrastructure

Infrastructure contains outward adapters:

- `store/` owns shared PostgreSQL, Redis, and transaction resources.
- `iam/` implements IAM domain ports.
- `ent/` contains Ent schema and generated persistence code.

Infrastructure may import Domain to implement its ports. It must not import
Application or Proto DTO packages.
