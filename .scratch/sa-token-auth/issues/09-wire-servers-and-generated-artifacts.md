# Complete wiring, server registration, and generated artifacts

Type: task
Status: done 2026-08-17
Blocked by: 05, 06, 07, 08

## Objective

Assemble the completed auth feature through Wire and verify that HTTP starts
only with a complete, valid policy/configuration graph.

## Work

- Register new repo, usecase, service, middleware, and Redis providers in the
  existing ProviderSets.
- Register auth/online services in the HTTP server and install middleware in the
  confirmed order.
- Leave gRPC enforcement out of this release, but do not introduce HTTP types
  into biz/data interfaces.
- Validate auth config and the exclusive-edge-proxy assertion at startup.
- Ensure cleanup closes Manager resources and Redis independently and safely.
- Run `make all` to refresh proto, config, Wire, ent, and module artifacts as
  needed; never hand-edit generated files.
- Update example config/comments without adding credentials.

## Acceptance

- The application starts with valid config and refuses missing/malformed policy
  or unsafe proxy configuration.
- HTTP endpoints are protected exactly as declared; no existing endpoint remains
  accidentally unauthenticated.
- `wire_gen.go` and generated proto/config files are reproducible from sources.
- `go test ./...` passes from a clean checkout with required test services.
