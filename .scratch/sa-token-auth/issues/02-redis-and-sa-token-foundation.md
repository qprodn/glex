# Add the Redis and sa-token data foundation

Type: task
Status: done 2026-08-15
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

## Comments

- 2026-08-15 implemented. **Version deviation:** the spec pins sa-token-go
  v0.2.4, but no such tag exists upstream (`github.com/sa-tokens/sa-token-go`
  latest is v0.2.3; verified via Go proxy and `go mod download` against the
  upstream git tags). Pinned **core v0.2.3**; the spec's v0.2.4 behavioral
  notes were re-verified against v0.2.3 source (`Manager.Login` rewriting the
  Account Session, `adapter.Storage` shape, `MaxLoginCount` overflow
  behavior). Redis client: `github.com/redis/go-redis/v9` v9.19.0 — the same
  driver the upstream `storage/redis` module uses.
- Adapter tests run on miniredis + a black-hole TCP listener (bounded-timeout
  proof); no real credentials required. Real-Redis coverage stays with ticket
  10. `NewAuthStorage`/`NewAuthManager` are registered in `data.ProviderSet`
  but enter the Wire graph only once ticket 03/04 consumers exist; config
  validation is unit-tested, and cleanup is covered by the client lifecycle
  tests (`Close` makes every later operation fail) — `CloseManager` itself is
  a no-op while no renew pool is configured.
- Metrics hooks: the repo has no metrics stack yet; `observe` is the single
  seam (log now, metrics attach there later). The Redis client fails startup
  when Redis is unreachable (fail-fast over per-request 503s) — accepted
  behaviour beyond the ticket text.
- Review fixes applied: `SetKeepTTL` is a single atomic `SET ... KEEPTTL XX`
  (no check-then-act resurrection window); `Clear` bounds every scan page and
  unlink batch with its own timeout context.
- Logs and `ErrAuthKeyNotFound` messages carry only the key class
  (`token:` / `account:`), never the raw bearer token (spec: never log the
  credential); a regression test captures slog output to pin this.
- Fixed pre-existing `go vet` copylocks failures in
  `api/system/auth/v1/policy_test.go` (proto message values copied by value).
