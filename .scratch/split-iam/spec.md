# Split iam into user / access / authn

Split the single iam domain into three modules inside the one service, drawn to
service-extraction standard. Decisions are recorded in
`docs/adr/0016-split-iam-into-user-access-authn.md` and
`docs/adr/0017-crossdomain-anti-corruption-layer.md`; the glossary lives in
`CONTEXT.md` (three sections: user, access, authn). Cross-module contracts
follow the Coze-style anti-corruption shape: every cross-module call goes
through `internal/crossdomain/<provider>` (contract.go, model/, impl/, mock),
consumed by constructor injection.

Migration shape (per ADR 0016): two prefactor tickets land green on main; the
physical move proceeds module-by-module on an integration branch; the final
ticket verifies and merges the branch to main as a single split PR — main never
sees an intermediate state.

Tickets live in `issues/`, numbered in dependency order. Frontier to start:
01 and 02.
