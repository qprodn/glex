# Add the Redis and sa-token data foundation

Type: task
Status: ready-for-agent
Blocked by: 01

## Objective

Create the shared Redis client, project-owned sa-token Storage adapter, and
Manager provider without exposing them outside `internal/data`.

## Work

- Pin `sa-token-go` v0.2.4 and the selected Redis client dependency.
- Extend `Data` to own one long-lived Redis client and close it during cleanup.
- Implement `core.Storage` with the configured glex auth namespace, bounded
  operation timeouts, missing-key classification, metrics/log hooks, and no
  application path that can clear the whole Redis database.
- Explicitly validate the sa-token config before `core.NewManager`.
- Configure `TokenStyleSimple`, `IsConcurrent=true`, `IsShare=false`, fixed
  seven-day expiry, and explicit active-timeout support. Do not rely on
  sa-token's `MaxLoginCount`.
- Add data-layer wrappers that preserve kickout/replaced/expired/storage-failure
  distinctions rather than using bool-only helpers.
- Register providers through `data.ProviderSet` and Wire.

## Acceptance

- Adapter contract tests cover set/get/delete, TTL, keep-TTL, missing keys,
  backend timeout, namespace isolation, and cleanup.
- No `stputil` or `integrations/kratos` import exists.
- No server or biz package imports the Redis driver or `*core.Manager`.
- `go test ./...` and adapter tests against real Redis pass.
