package wechatinfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	wechat "glex/internal/domain/wechat"
	"glex/internal/infra/wechat/crypt"
)

// deletedAt stamps the soft-delete time (ADR 0002 — no row is hard-deleted).
func deletedAt() time.Time {
	return time.Now()
}

// isMasterKeyMissing reports whether err carries the crypt sentinel.
func isMasterKeyMissing(err error) bool {
	return errors.Is(err, crypt.ErrMasterKeyMissing)
}

// wechatStoreError is the log-and-map for unexpected wechat-store failures
// (ADR 0007): the raw error is logged once at the handling site and returned
// as the module's sentinel. Expected business conditions map silently at
// the call sites. A canceled request is benign and returns the sentinel
// without logging.
func wechatStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return wechat.ErrWechatStoreUnavailable
	}
	logger.ErrorContext(ctx, "wechat store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return wechat.ErrWechatStoreUnavailable
}
