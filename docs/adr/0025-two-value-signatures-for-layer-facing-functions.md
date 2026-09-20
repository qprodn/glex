# Two-value signatures for layer-facing functions

A signature inventory (2026-09-20, 979 exported declarations) found 236
functions taking `context.Context` plus two or more parameters —
`Delete(ctx, id, actorID)`,
`MarkSuccess(ctx, id, paymentID, amount, channelRefundNo, refundedAt)` —
and 13 returning two-plus values (`(*T, bool, error)` exist flags,
`(*Dept, *Dept, error)` pre/post images), with parameter objects split
across three dialects (proto `XxxRequest` at the application layer, pay's
hand-written `XxxInput`, crossdomain's `model.XxxRequest`). Every added
field rippled interface → impl twin → mock → call site. Decided:
**a layer-facing exported function takes at most `context.Context` plus one
business object and returns at most one value plus `error`** — application
services, domain services, repository implementations, and crossdomain
contracts; bizpkg follows by convention.

The rules that keep it uniform:

- **Scalars count as the one object** (`FindByID(ctx, id)`,
  `Delete(ctx, id) (int64, error)` — coze-studio's own repository layer
  precedent; this codebase is modeled on its layout). Wrapping every id in
  a struct buys ceremony, not safety.
- **Two business values travel in one struct**: parameters ride an
  `XxxRequest` type (variable `req`) declared beside the consuming
  interface — the dialect coze-studio's application and user-domain layers
  already use. pay's `XxxInput` family renames to match, leaving one
  suffix repo-wide; proto and `model` requests are untouched.
- **Missing is `nil` (or the module sentinel), never a bool flag**: exist
  flags (`Owns`, `ValidateSession`) become nil-means-absent; lookup
  comma-ok in the pay channel catalog becomes a sentinel over the zero
  value.
- **One result struct beats two returns**: pre/post images
  (`UpdateDept`), resolved pairs (`ResolveGateway`, `File.Open`) ride one
  struct. A list page window rides one page struct (`listq.Page[T]` when
  the first need appears; none exists today — domain Lists return
  `(items, error)` and applications mint tokens).
- **Package-level invariant helpers unexport instead of conforming**:
  `Assert*`, `RecordInvalidLogin` are package internals reaching for repos,
  not layer API; they leave the exported surface rather than grow a
  request struct. If a cross-package caller ever appears, merge params
  instead.
- **Exempt**: `New*` constructors (dependency lists, including
  `NewAuthManager`'s cleanup func), entity and value-object methods,
  variadic `opts ...Option`, and generated code (mockgen mocks regenerate,
  never hand-edit). `InTx(ctx, fn)` is compliant by arity — the closure is
  the one object.
- **authn's `ListBySystemUser(ctx, id, opts ...ListOption)`** folds into
  one query struct, retiring the exemption ADR 0024 granted the online
  session listing's option family.
- Enforcement is review-based, like ADR 0007/0011 — no lint.

Migration is tracer-bullet per module, smallest first (audit → dict →
user → file → job → org → access → authn → pay), one commit per module,
`go build ./...` + module tests green between hops. `pkg/excelx.Translator`
and `pkg/middleware/auth` mirror domain shapes and move with the same
commits though `pkg/` sits outside the enforced scope.
