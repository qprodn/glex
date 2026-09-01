package authninfra

import (
	"glex/internal/infra/store"

	"context"
	"log/slog"

	authnrepo "glex/internal/domain/authn/repository"
)

type credentialRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewCredentialRepo constructs the credential store — the only writer of the
// credential column on the System User row (ADR 0005).
func NewCredentialRepo(data *store.Data) authnrepo.CredentialRepo {
	return &credentialRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.authn.credential")),
	}
}

// StoreCredential replaces the stored password hash inside the caller's
// propagated transaction.
func (r *credentialRepo) StoreCredential(ctx context.Context, systemUserID int64, passwordHash string) error {
	if err := r.data.DB.User(ctx).UpdateOneID(systemUserID).
		SetPassword(passwordHash).
		Exec(ctx); err != nil {
		return authDatabaseBackendError(ctx, r.logger, "authn.store_credential", err)
	}
	return nil
}
