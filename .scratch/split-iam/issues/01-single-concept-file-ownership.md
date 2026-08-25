# 01 — Prefactor: give every iam source file a single concept owner

**What to build:** Re-home the flat shared files inside the existing iam
packages so that every source file maps to exactly one future module (user /
access / authn per ADR 0016): the single error block splits by concept so each
error sits beside the code that raises it; the security-change classification
methods (which change revokes which Login Sessions) move beside the domain
objects they classify; the password-hashing helpers move beside their
consumers. Pagination/list-option helpers stay put for now. This is pure file
reorganization inside the same packages — zero behavior change — so the
physical move later becomes per-file renames that git can track.

**Blocked by:** None — can start immediately.

**Status:** done

- [x] No source file in the iam packages mixes concerns from two future modules
- [x] Security-change classification remains pure domain logic with unchanged behavior
- [x] `make all` and the full test suite stay green; tests change imports/references only

## Comments

Implemented as pure in-package file re-homing (zero behavior change):

- `domain/iam/errors.go` split: user errors → `user.go`, role errors →
  `role.go`, menu errors → `menu.go`, session/token/backend state errors →
  `login_session.go`, login-flow errors (`ErrInvalidCredentials`,
  `ErrSystemUserDisabled`, `NewLoginLocked`) → `auth_login.go`, and the
  credential/permission-seam errors (`ErrCredentialMissing`,
  `ErrCredentialMalformed`, `ErrPermissionDenied`) → `principal.go`.
- `domain/iam/security_change.go` split: `AuthorizationChangedFrom` moved
  beside `Role`/`Menu`, `SessionRevocationFrom` beside `User`, and the
  `SessionRevocation` policy + `SystemUserSessionRevocation` +
  `SessionRevoker` into `login_session.go` (authn-owned; ticket 02 promotes
  the revoker to crossdomain). `security_change_test.go` split into
  `role_test.go` / `menu_test.go` / `user_test.go`, tests verbatim.
- `application/iam/password.go` split: `hashPassword` → `user_usecase.go`
  (its only consumer), `passwordMatches` + `invalidCredentialHash` →
  `auth_usecase.go` (login verification).
- Left for later tickets by design: `pagination.go` (stays per ticket),
  `application/iam/security_change.go` revocation helpers (ticket 02), the
  authn/access span of `AuthUsecase`/`AuthApplication` (tickets 03/04/06 —
  the current proto service shape owns both endpoint families).
