# 06 — Split the public proto contracts

**What to build:** Split the external contracts off api/iam into
`api/user/v1`, `api/access/v1`, and `api/authn/v1`. access gets the Role and
Menu services plus a new standalone RoleAssignmentService hosting the three
assignment RPCs (ListUserRoles, UpdateUserRoles, ListRoleUsers) moved off the
auth service; authn gets login/logout plus the online-session service; user
keeps the user service unchanged. Regenerate the stubs; application adapters,
server registration, and the auth-policy route table follow the new operation
identifiers. Write the short client-migration note: new import paths, and the
assignment RPCs' new home.

**Blocked by:** 05 — rename remainder to user.

**Status:** ready-for-agent

- [ ] api/iam is gone; the three proto trees exist with the agreed service grouping, including a standalone RoleAssignmentService
- [ ] Application adapters, server registration, and the route table point at the regenerated contracts; middleware behavior unchanged
- [ ] HTTP contract tests re-pinned to the new operations and green
- [ ] Wire regenerated; `make all` builds on the branch
