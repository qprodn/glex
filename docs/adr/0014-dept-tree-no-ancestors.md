# The department tree has no materialized ancestors path

`sys_dept` stores only `parent_id` (0 = root). Trees, subtrees, and
parent-select exclusions are derived in the org domain service by loading the
active departments and walking them in memory — the same shape as access's
`BuildMenuTree`. RuoYi-Vue-Plus's `ancestors` column (the comma path
`"0,100,101"`) is deliberately not ported (decided 2026-09-04).

RuoYi needs ancestors for three constraints glex does not share:

1. Its data-permission engine is a SQL-rewriting interceptor (JSqlParser).
   A `DEPT_AND_CHILD` scope must be expressible inside the rewritten SQL —
   `dept_id IN (select dept_id from sys_dept where find_in_set(?, ancestors))`
   — so subtree membership has to be a column the rewriter can read. glex has
   deferred data permission, and its permissions already resolve at the domain
   layer (Authorization Resolution → Authorization Snapshot), so a future
   department scope would compute descendant ids in the org domain and pass
   them into the query as parameters.
2. RuoYi ships against MySQL 5.7 plus Oracle, SQL Server, and PostgreSQL
   dialects; a recursive CTE is not portable across that matrix. glex is
   PostgreSQL-only, where a recursive CTE is native if SQL-level subtree
   resolution is ever wanted.
3. RuoYi's dept list is a flat response and the frontend builds the tree in
   one pass, leaning on `ORDER BY ancestors, parent_id, order_num` so parents
   arrive before children. glex's dept list is a server-built tree sorted by
   `order_num` in the domain, so no flat-ordering trick is needed.

## Considered Options

- **Chosen:** `parent_id` + in-memory derivation. Moving a department is a
  one-row update with nothing to cascade; dept tables are tens to hundreds of
  rows, the same scale menu already works at.
- **Rejected:** port `ancestors`. Fidelity to the reference, but it is derived
  data that must be maintained transactionally: every move rewrites the path of
  all descendants (RuoYi's `replaceOnce` batch update exists precisely to not
  corrupt deeper paths — a whole bug class), `varchar(500)` caps depth at
  roughly 20 levels with 19-digit snowflake ids, and the literal `"0"` sentinel
  leaks into domain logic. It buys nothing glex queries need.

## Consequences

- "Move department" is an ordinary edit of `parent_id`; no batch rewrite and no
  second source of truth that can disagree with it.
- Subtree membership is computed by the org domain service; cross-module
  consumers (the user list's department filter) get descendant ids through the
  org contract rather than by reading a path column.
- The scale ceiling is the in-memory load of all active departments. If that
  ever breaks, the PostgreSQL answers are a recursive CTE over `parent_id` or
  `ltree` — not a hand-maintained path column.
