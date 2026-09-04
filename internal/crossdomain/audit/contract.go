// Package audit publishes the audit module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, model/ holds flat record types, impl/ adapts the provider's
// domain capability, and auditmock holds the mockgen-generated test double.
// Contracts are consumed by constructor injection into the calling
// application service; there is deliberately no package-level service
// locator, and cmd stays the only composition root.
package audit

import (
	"context"

	"glex/internal/crossdomain/audit/model"
)

//go:generate go tool mockgen -package auditmock -destination auditmock/login_log_writer.go glex/internal/crossdomain/audit LoginLogWriter

// LoginLogWriter records one sign-in or session-end event in the audit
// module's Login Log (ADR 0012). authn is the only consumer — it is the
// module that knows whether a login attempt succeeded, why it failed, and
// who forced a session to end. The write is best-effort at the call site:
// a recorder failure must never fail the operation being recorded.
type LoginLogWriter interface {
	RecordLoginLog(ctx context.Context, request *model.LoginLog) error
}
