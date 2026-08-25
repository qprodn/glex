# Cross-module contracts follow a Coze-style anti-corruption layer

We studied Coze Studio's backend `crossdomain` layer (see
`.scratch/research-coze/coze-studio-backend-organization.md`) and adopt its
shape for glex's module split. Every `internal/crossdomain/<provider>` package
is named after the providing module and has a fixed layout: `contract.go` with
a consumer-shaped minimal interface, `model/` holding flat snapshot DTOs,
`impl/` whose adapter wraps the provider's domain capability to satisfy the
contract, and a mockgen-generated mock package for tests. Adapters live in the
crossdomain impl package, not in infra.

This supersedes two rulings in ADR 0016. First, cross-module contracts no
longer wait for a two-caller promotion bar: every cross-module call goes
through the layer regardless of caller count, so the Authorization Resolution
contract — not just revocation — is published at `internal/crossdomain/access`
and authn's login Usecase consumes it without importing `domain/access`.
Second, contracts return only `model/` snapshots, never provider domain
objects: unlike Coze, which type-aliases provider entities into its contracts,
glex forbids the alias so consumers never import the provider's domain package
at all.

One deliberate deviation from Coze: no package-level `defaultSVC` /
`SetDefaultSVC` service locator. Coze needs it because it hand-wires
dependencies and calls contracts from deep inside other domains' code; glex
uses Wire, its cross-module call sites are Usecase-level, and cmd remains the
only composition root. Consumption is constructor injection of the contract
into the calling Usecase, tests inject the generated mocks or hand fakes, and
domain packages still import nothing from crossdomain.
