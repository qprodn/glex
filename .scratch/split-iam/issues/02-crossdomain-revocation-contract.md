# 02 — Prefactor: stand up the Coze-style crossdomain revocation contract

**What to build:** Create `internal/crossdomain/authn` in the anti-corruption
shape recorded in ADR 0017: `contract.go` with the consumer-shaped
SessionRevoker interface, a `model/` package holding the flat revocation
request snapshot (affected System Users, optionally the Login Session to
preserve), an `impl/` package whose adapter wraps the login-session capability
to satisfy the contract, and a mockgen-generated mock package. The four
revocation-consuming usecases switch to the constructor-injected contract.
Security-change classification stays pure domain logic; caller usecases
assemble the request. This lands green on main and validates the layer's shape
before any physical move.

**Blocked by:** None — can start immediately (independent of 01).

**Status:** ready-for-agent

- [ ] `internal/crossdomain/authn` has contract.go, model/, impl/, and a generated mock subpackage
- [ ] The adapter lives in the crossdomain impl package, wrapping the provider capability; infra no longer owns the revoker narrowing
- [ ] All revocation-consuming usecases receive the contract by constructor injection; the old domain-owned port is gone
- [ ] No domain package imports crossdomain; the contract exposes only model/ snapshots, never provider domain types
- [ ] `make all` green; security-change integration tests stay green with unchanged revocation behavior
