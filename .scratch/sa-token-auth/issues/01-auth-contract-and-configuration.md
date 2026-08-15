# Define the authentication contract and configuration

Type: task
Status: ready-for-agent

## Objective

Define every proto/config surface required by the confirmed spec before writing
runtime code.

## Work

- Add shared auth-policy method options under `api/system/auth/v1` with
  `PUBLIC`, `OPTIONAL`, `AUTHENTICATED`, and `PERMISSION` modes.
- Annotate every existing and new RPC. `UNSPECIFIED` must remain distinguishable
  so runtime completeness validation can reject it.
- Add auth error reasons for login, token state, permission denial, and backend
  unavailability without leaking storage-driver text.
- Add `Login`/`Logout` and the four RuoYi online-session operations using the
  exact HTTP routes listed in `spec.md`.
- Add `/system/user/getInfo` to the user contract and
  `/system/menu/getRouters` to the menu contract.
- Model Login, Principal/current-user, online Session, and router DTOs. Never put
  the password hash or bearer token in a response.
- Extend `internal/conf/conf.proto` for the fixed/idle lifetime, five-Session
  limit, login-counter thresholds/windows, Redis auth namespace, and the
  deployment assertion that the edge proxy supplies the authoritative source
  IP.
- Update `configs/config.yaml` with non-secret defaults: 7 days, 30 minutes, 5,
  five failures/10 minutes, and twenty IP failures/10 minutes.
- Run repository generators; do not hand-edit generated files.

## Acceptance

- Every registered existing/new RPC has an explicit policy annotation.
- Login/logout and online routes match the table in `spec.md`; no public bulk
  logout RPC exists.
- `go test ./...` passes after generation.
- A descriptor-level test can enumerate policy metadata and identify malformed
  `PERMISSION` declarations.

## Notes

Follow AGENTS.md layering and generation rules. This ticket defines contracts;
it does not implement authentication.
