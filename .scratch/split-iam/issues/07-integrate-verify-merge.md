# 07 — Integrate, verify, and merge the split

**What to build:** Finish the split as one PR. Re-home the whole-graph
integration suites so they construct the three modules together — they are the
safety net for the cross-module seams (login → snapshot, security change →
revocation). Update the architecture/layering contract tests for the new
package prefixes and the crossdomain package, encoding ADR 0016's boundaries.
Update AGENTS.md and the layer READMEs: Principal now lives at
`domain/authn`, directory examples show the three modules, and the crossdomain
rules describe the anti-corruption layer shape — per-provider
contract/model/impl/mock packages, every cross-module call through the layer,
constructor-injected consumption, no service-locator defaults (ADR 0017). Run the full suite including the env-gated
integration tests, then merge the integration branch to main as the single
split PR — main never saw an intermediate state.

**Blocked by:** 06 — split proto contracts.

**Status:** ready-for-agent

- [ ] Integration suites cover login → Authorization Snapshot → revocation and cross-module security changes against the new structure
- [ ] Architecture/layering tests encode the ADR 0016 boundaries, including that domain imports nothing new
- [ ] AGENTS.md and layer READMEs match the new structure; glossary already consistent with CONTEXT.md
- [ ] `make all` and the full test suite (including env-gated integration) green on the branch; single PR merged to main
