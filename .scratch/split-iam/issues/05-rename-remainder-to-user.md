# 05 — Rename the remaining iam packages to user

**What to build:** Rename what remains of iam to the user module: System User
profiles, GetInfo, and the user-side orchestration (for example, changing a
password calls authn's credential capability plus revocation). Distribute the
shared plumbing that ticket 01 left in place: each module gets its own errors
and list-option helpers beside its code, and each application module owns its
transaction port — all three bound to the same infra transaction manager at
composition time. No generic shared package is created.

**Blocked by:** 04 — extract access.

**Status:** ready-for-agent

- [ ] No iam-named packages remain; the user module contains only profile concerns plus its orchestration
- [ ] Errors and list-option helpers are owned per module; no shared/common package was introduced
- [ ] Each application module declares its own transaction port, all bound to the one infra transaction manager
- [ ] Branch compiles and module-scoped tests pass
