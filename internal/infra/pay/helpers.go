package payinfra

import (
	"errors"
	"time"

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
