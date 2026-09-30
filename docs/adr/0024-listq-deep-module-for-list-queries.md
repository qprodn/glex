# pkg/listq deep module for list queries

Sixteen application List methods across thirteen files carried the same
~35-line stanza — declare AIP filter idents, `ParseFilter`,
`ParsePageToken`, `ParseOrderBy`, default order, default page size, call,
mint the next token — and nine modules carried byte-identical
`repository/list_option.go` copies (~375 lines) that domain List forwarders
repacked four arguments into and ent repos immediately unpacked. The copies
differed only in data (the declared idents, the default order), never in
control flow; the repo side was already generic through
`ents.ApplyFilter`'s field-name convention. Decided 2026-09-20: **collapse
the stanza into `pkg/listq`, a stateless deep module** — `Parse(request,
Spec)` returns a `Query` value (filter, order, limit, offset) plus the page
window, `Next` mints the next token — and delete the sixteen stanzas and
nine option files; `Query` flows application → domain → repo as one value,
replacing the four-argument repack.

The rules that keep it deep:

- **Spec is the only per-module fact**: the filterable idents plus the
  default order. Adding a filterable field is one declaration line.
- **Placement is `pkg`, not bizpkg** (ADR 0010): bizpkg may be imported by
  application services only, and `Query` must reach domain services and
  infra repos. listq imports nothing from `internal` — the same family as
  `pkg/validate`, `pkg/excelx`.
- **The helper stops at `([]T, nextToken)`**: callers assemble their own
  Set. Collection field names differ (`Payments`, `Users`, `Logs`), and
  absorbing them would cost reflection or callbacks for three lines.
- **Bespoke filters stay explicit typed parameters**: the user dept-subtree
  filter resolves in the application and rides the domain List as
  `deptIDs`, never rewritten into the filter string — the AIP filter is the
  caller's wire convention, and server-side rewriting would split "what
  this filter supports" into two places.
- **Page policy lives here**: default 20 (the value all sixteen copies
  used), shared `MaxPageSize = 500` — the web app's observed need tops out
  at 200 (merchant and form dropdowns); beyond the cap is an invalid
  argument, on the same path as a bad filter.
- **Pure function, no ctx/logger**: callers log the one ADR 0011 breadcrumb
  under their own operation (`pay.list_payments`), so the trail stays
  module-scoped.
- **Exemption**: authn's online-session listing keeps its current shape
  until its Redis repo's List contract is narrowed (a separate review
  item); it honors two idents with string equality and cannot stand behind
  a full Spec. **Retired by ADR 0025**: the listing's option family folds
  into one query struct like every other layer-facing signature.

Migration is tracer-bullet: dict first (smallest, all three tiers), then
pay, then the rest. Existing module tests stay — they exercise the real
parse path — and table-driven tests land in `pkg/listq`.
