# 04 — Extract the access module

**What to build:** Move Roles, Menus, Menu Grants, Role Assignment (the link
storage and both of its endpoints, including the application side of the three
assignment RPCs), and Authorization Resolution into the access module across
all three layers. The resolution contract is published at
`internal/crossdomain/access` in the same anti-corruption shape as the
revocation contract (contract.go, model snapshot, impl adapter, mock, per ADR
0017); authn's login consumes the injected contract to fill the Authorization
Snapshot and never imports access domain types. iam shrinks to System User
concerns. When this ticket lands, role, menu, and assignment flows work
end-to-end on the branch, and login produces snapshots via the crossdomain
contract.

**Blocked by:** 03 — extract authn.

**Status:** ready-for-agent

- [ ] Role Assignment's storage and both endpoints (by user and by role) live in access; user flows orchestrate through access
- [ ] The Authorization Resolution contract lives at `internal/crossdomain/access` exposing only model snapshots; authn's login consumes it by injection, imports no access domain package, and derives no permissions itself
- [ ] Role/menu/assignment flows pass their tests on the branch; branch compiles with no behavior change
