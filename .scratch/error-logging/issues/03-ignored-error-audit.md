# 03 - Audit explicitly ignored errors (`_ = err`)

Status: ready-for-agent

Audit every explicit ignore in `internal/` and `pkg/` (~10+ sites, e.g.
`internal/infra/authn/auth_login.go:144-148` best-effort logout cleanup,
`internal/infra/access/snowflake.go:9` node init). Per ADR 0007 each site
gets one of:

- a comment justifying why the silence is safe, or
- a `Warn` log with `operation` + `slog.Any("error", err)` when the failure
  is worth knowing about (e.g. logout cleanup dropping sessions).

Rule: ADR 0007. Acceptance: no bare `_ =` on an error-bearing call without
an adjacent justification comment or a Warn; `go build ./...` passes.
