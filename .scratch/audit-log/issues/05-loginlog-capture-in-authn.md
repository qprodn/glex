# 05 — Login Log capture in authn (crossdomain contract)

Status: resolved

`internal/crossdomain/audit`: `LoginLogWriter` contract
(`RecordLoginLog(ctx, *model.LoginLog) error`), impl over the audit domain
service, mockgen mock. authn application records, best-effort (Warn on
failure, never blocks the operation):

- Login success / failure (reason as msg) in `LoginApplication.Login`
- Logout in `LoginApplication.Logout`
- admin 强退 in `KickoutOnlineSession` (create_by = the admin, subject =
  the kicked user)
- self-revoke in `DeleteMyOnlineSession`

Browser/OS reuse `loginClientMetadata`; IP from the same extraction the
login flow already does (fallback X-Forwarded-For read where absent).

## Accept

- authnmock gains the writer; login/online-session tests pin the
  recorded events and the best-effort behavior.

## Comments

- 2026-09-04: Implemented: crossdomain/audit LoginLogWriter (mockgen mock, model snapshot, impl detaches context + 3s timeout per ADR 0012). authn records: login success/failure (authenticate), logout, admin 强退 (KickoutOnlineSession — now reads the session first, so kicking an unknown id fails instead of silently succeeding), self-revoke (DeleteMyOnlineSession). OnlineSession domain service gained Get(ctx, sessionID) for the metadata read. authn tests pin the rows and the best-effort behavior.
