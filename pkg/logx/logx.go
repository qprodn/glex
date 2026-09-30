// Package logx holds the structured-logging helpers shared by the domain
// and application layers (ADR 0011). It is stdlib-only so both layers may
// import it without touching the dependency rule.
package logx

import (
	"context"
	"errors"
	"log/slog"
)

// Propagate logs the Warn breadcrumb for an error a domain service or
// application service is about to return to its caller unchanged
// (log-and-return, ADR 0011). `Error` stays reserved for handling sites
// (ADR 0007): one failure reads as a Warn trail — domain step, application
// use case — capped by the infra log-and-map and the boundary record.
// operation is the dotted path of the propagating site, e.g.
// `authn.login` for an application use case or `authn.svc.verify_password`
// for a domain service step. A canceled request is benign and logs nothing.
func Propagate(ctx context.Context, logger *slog.Logger, operation string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	logger.WarnContext(ctx, "propagating error", slog.String("operation", operation), slog.Any("error", err))
}
