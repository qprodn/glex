# Application calls domain services, never repositories

Until now the Application layer held repository ports directly: all six
handlers imported `domain/*/repository` (~41 call sites), and the seam
comments made that the contract — "Application Usecases are transaction
roots… must pass ctx to every repository call", "The Application service
owns the transaction" — with `domain/*/service` as free functions that took
repos as parameters. Decided 2026-08-28: **overturn that contract and adopt
coze-studio's layering** — `api handler → application → domain service →
repository port → infra`. Application business code holds **zero repository
references**; every read and write goes through a domain service interface,
and repo construction stays in infra fx providers (the equivalent of coze's
`init.go` composition roots). Reads included: a writes-only rule would rot,
so thin pass-through service methods (`List` delegating straight to the
repo) are the accepted price of one uniform, lint-enforceable rule.

Services as they stood at this decision — 2026-08-28, when only three modules
existed (access, authn, user) — one per application handler: **Role** (holds
the role, menu, and user-role repos), **Menu**, **RoleAssignment**, **Login**,
**OnlineSession**, **User**. Every module added since owns service(s) of its
own under the same rule; the enumeration is a snapshot, not the current set.
Two further seam services exist only for crossdomain delegation:
**Authorization** (Authorization Resolution) and **Credential** (password
hashing and store) — the crossdomain impls delegate to services, not repos. Interface and implementation live together in
`domain/<mod>/service/` (coze's `message.go` + `message_impl.go` style) with
plain Go names (`service.Role`), wired by fx — no coze-style global `SVC`
singletons. Repos may be held by several services (coze's knowledge SVC
holds four). Free functions that take a repo parameter (`AssertRoleUnique`,
`AssertMenusGrantable`, …) fold into the owning service's methods; pure
functions (`CheckedMenuIDs`, `Hash`/`VerifyPassword`) stay as free functions
and remain callable from application. Service methods take and return
domain entities only — proto mapping stays in application. Crossdomain
impls stop adapting repositories and delegate to domain services instead
(coze's ACL pattern), so no use-case-adjacent code touches a repo. One
transport-level exception stays by design: the server's auth middleware
consumes authn's `Authenticator` port directly — token→Principal plumbing,
not a use case, and not covered by the depguard rules.

Two deliberate deviations from coze are part of this decision:

1. **The transaction root stays in Application.** Coze owns transactions in
   the domain layer, but propagates them as raw `*gorm.DB` /
   `*query.QueryTx` parameters — through repo interfaces
   (`InitTx() (tx *gorm.DB, …)`) and even a crossdomain contract — in three
   coexisting styles. glex's ctx-carried `Transaction.InTx` port is cleaner
   and needs zero changes: application wraps the use case, services and
   repos are tx-oblivious, repos join via ctx. Consequence: a multi-write
   service method is atomic only when its caller wrapped it in `InTx` —
   that discipline cannot be checked by compiler or lint.
2. **No structural `internal/` moves.** Repo impls stay in `infra/`; they
   are reachable only through fx providers, so moving them under
   `domain/<mod>/internal/` for compiler enforcement would be churn without
   a new guarantee.

Enforcement is three golangci-lint depguard rules: `internal/application/**`
must not import `internal/domain/*/repository`; `internal/crossdomain/*/impl/**`
likewise; `internal/domain/<mod>/**` must not import another module's
domain (cross-module calls go through crossdomain contracts only).

## Considered Options

- **Chosen:** full coze-style purity — all repo access behind domain
  services, reads and writes alike, fx-wired, depguard-enforced.
- **Rejected:** keep the existing application-owns-repos contract. It was
  deliberate, not accidental, but a house style split between the two models
  is the worst state; we picked one.
- **Rejected:** writes via services, reads via repo ports. Split rules rot —
  the next `List` gets written by copying whatever was nearest.
- **Rejected:** coze's transaction placement (domain-owned, explicit tx
  params). Takes raw gorm handles onto interfaces; glex's ambient ctx
  mechanism is better and already exists.

## Consequences

- ~41 repo call sites move out of six handlers; handler tests now mock
  service interfaces, service tests mock repo ports — same mock count,
  relocated.
- Domain checks (uniqueness, guards) now run inside the use-case
  transaction's snapshot rather than before it — same outcomes, one
  transaction open even on a validation failure.
- Thin read pass-throughs on services are expected and fine.
- Multi-write atomicity depends on the application remembering `InTx`;
  this ADR is the place that obligation is recorded.
- This ADR supersedes the seam-comment contracts in
  `application/*/transaction.go` and the repo port comments. Comments citing
  ADR 0013/0016/0017 (numbers ported from coze's docs that never existed
  here) are repointed to this ADR; no backfilled stubs.
- GLOSSARY.md is untouched: this is architecture, not domain vocabulary.
