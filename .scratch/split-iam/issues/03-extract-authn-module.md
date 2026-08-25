# 03 — Extract the authn module onto the integration branch

**What to build:** Open the integration branch for the split and carve out the
authn module across all three layers: Credentials (hash, policy, verification),
Login Sessions and their tokens, Principal, the login/logout and
online-session usecases, and the sa-token machinery. The remaining iam code
reaches authn only through the crossdomain revocation contract and the
Principal context. Login's Authorization Snapshot filling may still resolve
authorization from the iam side — that flips in ticket 04. When this ticket
lands, login, logout, and online-session flows work end-to-end through the
whole stack on the branch.

**Blocked by:** 01 — single-concept file ownership; 02 — crossdomain revocation contract.

**Status:** ready-for-agent

- [ ] Credential and Login Session ownership live in authn per ADR 0016; the credential column on the user record is read/written only by authn's adapter
- [ ] Remaining iam code calls authn only via the crossdomain revocation contract and Principal
- [ ] Login/logout/online-session flows pass their unit and infra tests on the branch; branch compiles with no behavior change
