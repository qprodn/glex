package authninfra

import (
	"context"
	"fmt"

	"glex/internal/infra/store"

	authndomain "glex/internal/domain/authn"
)

type credentialRepo struct {
	data *store.Data
}

// NewCredentialRepo constructs the credential store — the only writer of the
// credential column on the System User row (ADR 0016).
func NewCredentialRepo(data *store.Data) authndomain.CredentialRepo {
	return &credentialRepo{data: data}
}

// StoreCredential replaces the stored password hash inside the caller's
// propagated transaction.
func (r *credentialRepo) StoreCredential(ctx context.Context, systemUserID int64, passwordHash string) error {
	if err := r.data.DB.User(ctx).UpdateOneID(systemUserID).
		SetPassword(passwordHash).
		Exec(ctx); err != nil {
		return fmt.Errorf("store credential: %w", err)
	}
	return nil
}
