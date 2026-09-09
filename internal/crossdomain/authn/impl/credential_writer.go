package impl

import (
	"context"
	"fmt"

	"glex/internal/crossdomain/authn"
	"glex/internal/crossdomain/authn/model"
	authnservice "glex/internal/domain/authn/service"
)

// credentialWriter satisfies the credential contract by delegating to the
// provider's credential domain service: hashing happens there, beside the
// provider's domain policy, so consumers never pick a hash format.
type credentialWriter struct {
	credentials authnservice.Credential
}

// NewCredentialWriter adapts the authn credential domain service to the
// crossdomain credential contract.
func NewCredentialWriter(credentials authnservice.Credential) authn.CredentialWriter {
	return &credentialWriter{credentials: credentials}
}

// WriteSystemUserCredential stores the requested password inside the caller's
// propagated transaction.
func (w *credentialWriter) WriteSystemUserCredential(ctx context.Context, request *model.SystemUserCredential) error {
	if request == nil || request.SystemUserID <= 0 {
		return fmt.Errorf("write system user credential: system_user_id is required")
	}
	return w.credentials.Store(ctx, request.SystemUserID, request.Password)
}

// WriteSystemUserCredentials stores the shared password for the batch inside
// the caller's propagated transaction, hashing it once (ADR 0017).
func (w *credentialWriter) WriteSystemUserCredentials(ctx context.Context, systemUserIDs []int64, password string) error {
	for _, id := range systemUserIDs {
		if id <= 0 {
			return fmt.Errorf("write system user credentials: system_user_id is required")
		}
	}
	return w.credentials.StoreAll(ctx, systemUserIDs, password)
}
