package payinfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	pay "glex/internal/domain/pay"
	"glex/internal/infra/pay/crypt"
)

// deletedAt stamps the soft-delete time (ADR 0002 — no row is
// hard-deleted).
func deletedAt() time.Time {
	return time.Now()
}

// isMasterKeyMissing reports whether err carries the crypt sentinel
// (ADR 0018 — the key is held by the environment, never config.yaml).
func isMasterKeyMissing(err error) bool {
	return errors.Is(err, crypt.ErrMasterKeyMissing)
}

// payStoreError is the log-and-map for unexpected pay-store failures (ADR
// 0007): the raw error is logged once at the handling site with its
// operation and returned as the module's sentinel, so no raw ent error
// crosses the repository boundary. Expected business conditions
// (ent.IsNotFound, unique-violation dedup) map silently at the call
// sites; a canceled request is benign and returns the sentinel without
// logging.
func payStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return pay.ErrPayStoreUnavailable
	}
	logger.ErrorContext(ctx, "pay store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return pay.ErrPayStoreUnavailable
}

// truncateUTF8 bounds a channel-supplied text to max runes — byte slicing
// would split multi-byte characters.
func truncateUTF8(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
