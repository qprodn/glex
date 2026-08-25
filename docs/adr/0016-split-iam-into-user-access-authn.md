# Split the iam domain into user, access, and authn modules

The single `iam` package entangled authentication, authorization, and user
administration in every layer, and we want the option of extracting separate
services later. We split it into three modules inside the one service —
`user` (System User profiles), `access` (Roles, Menus, Menu Grants, Role
Assignments, permission resolution), and `authn` (Credentials, Login Sessions,
tokens, Principal) — as package-level boundaries drawn to service-extraction
standard: modules interact only through ports and snapshots, never through each
other's domain objects or storage.

Boundary rulings a reader cannot reconstruct from the code alone:

- Role Assignment (`sys_user_role`) and both of its endpoints belong to access;
  user flows that need assignments orchestrate through access.
- Authorization Resolution (assignments + grants → Role Keys + Functional
  Permissions) is owned by access. authn's login Usecase calls an access port
  to fill the Authorization Snapshot and never derives permissions itself.
- Credentials (password hash, policy, verification) belong to authn even though
  the hash column physically stays on `sys_user`; only authn's adapter reads or
  writes that column until a service split forces a dedicated credential table.
- Principal deliberately stays the fused identity+authorization carrier with
  literal `"*:*:*"` wildcard matching (ADR 0008's snapshot is one account-level
  thing). access decides who gets which permissions; Principal only answers
  set-membership. `pkg/middleware/auth` keeps its own Principal interface and
  context key; `domain/authn` bridges by type assertion, as `domain/iam` did.
- GetInfo stays on UserService (user) and GetRouters on MenuService (access);
  both read the Principal snapshot, adding no cross-module port.
- Tables are not migrated. Adapters are re-packaged by owner: `sys_user` →
  user; `sys_role`, `sys_menu`, `sys_role_menu`, `sys_user_role` → access;
  sa-token Redis state → authn.
- Cross-module contracts: SessionRevoker is promoted to
  `internal/crossdomain/authn` now, because it already has two callers (user,
  access) and meets AGENTS.md's promotion bar. Authorization Resolution has a
  single caller (authn) and therefore stays a port in access's domain. Domain
  packages import nothing new: security-change classification stays pure domain
  logic, and caller Usecases assemble the crossdomain revocation request.
- Proto paths follow the split (`api/user/v1`, `api/access/v1`,
  `api/authn/v1`), accepted as breaking because consumers are our own
  frontends. The three role-assignment RPCs move off AuthService into an
  access-owned service; Login/Logout and OnlineSessionService land in authn.
- Mechanical fallout follows AGENTS.md: per-module `Transaction` ports and
  `ListOption` helpers live beside their Usecases, errors split by owning
  module, and the move is one migration PR with Wire regenerated and the
  architecture/integration tests re-homed.

Considered and rejected: capability-owned ports for revocation with no shared
contract until a third caller appears (the two-caller bar is already met);
slimming Principal to pure identity (rewrites the middleware seam and every
`HasPermission` call site for conceptual purity); incremental three-step
migration (shared files like `errors.go` and `security_change.go` would pass
through awkward intermediate states); keeping `api/iam` proto paths (service
extraction would force the change anyway, and consumers can move in lockstep).
