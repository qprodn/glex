# Login Sessions share an account-level authorization snapshot

All Login Sessions for a System User will authorize from one sa-token account
Session containing a snapshot of the user's active Roles and Functional
Permissions, rather than query the database on every request. Each login has an
independent token but refreshes this shared snapshot. Changes to Role
Assignments, Menu Grants, authorization-bearing Role fields (`role_key`, status,
deletion), authorization-bearing Menu fields (`perms`, status, deletion), or
administratively controlled System User state revoke affected Login Sessions
through a `System User -> Login Sessions` index; a user changing their own
password may keep the current Session while the others are revoked. This accepts
forced reauthentication and shared-session write semantics in exchange for fast
request-time authorization and direct use of sa-token's permission APIs.

The project index, not sa-token's login-id/device mapping, is authoritative for
enumeration and the five-Session limit: v0.2.4 can overwrite a device mapping
while leaving its previous token valid. Registration, oldest-Session eviction,
and revocation therefore use project-owned atomic Redis operations, and the
wrapper deletes the shared snapshot when its index shows no remaining Sessions.
Moreover, v0.2.4 `Manager.Login` rewrites the whole account Session from an
initially empty value whenever it issues a new token. The login Usecase rebuilds
Roles and Functional Permissions in that account Session before returning the
token; no caller may use `Manager.Login` directly. Concurrent login and
authorization changes are best-effort, so an existing request can observe a
brief empty or stale snapshot. Data-scope authorization remains out of scope
until its own domain model exists.
