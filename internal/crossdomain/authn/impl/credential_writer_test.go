package impl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
)

// recordingCredentialRepo records stored hashes; the embedded interface stays
// nil because the adapter under test touches nothing else.
type recordingCredentialRepo struct {
	authndomain.CredentialRepo
	stored map[int64]string
	err    error
}

func (r *recordingCredentialRepo) StoreCredential(_ context.Context, systemUserID int64, passwordHash string) error {
	if r.stored == nil {
		r.stored = make(map[int64]string)
	}
	r.stored[systemUserID] = passwordHash
	return r.err
}

func TestCredentialWriterHashesAndStoresPlaintext(t *testing.T) {
	credentials := &recordingCredentialRepo{}
	writer := NewCredentialWriter(credentials)

	if err := writer.WriteSystemUserCredential(t.Context(), &model.SystemUserCredential{SystemUserID: 42, Password: "secret"}); err != nil {
		t.Fatalf("WriteSystemUserCredential: %v", err)
	}
	stored, ok := credentials.stored[42]
	if !ok {
		t.Fatal("no credential was stored")
	}
	if stored == "secret" || !strings.HasPrefix(stored, "$2") {
		t.Fatalf("stored credential = %q, want a bcrypt hash, not the plaintext", stored)
	}
	if !authndomain.VerifyPassword(stored, "secret") {
		t.Fatal("stored credential does not verify against the plaintext")
	}
}

func TestCredentialWriterRejectsMissingUser(t *testing.T) {
	credentials := &recordingCredentialRepo{}
	writer := NewCredentialWriter(credentials)

	if err := writer.WriteSystemUserCredential(t.Context(), &model.SystemUserCredential{Password: "secret"}); err == nil {
		t.Fatal("missing system_user_id must be rejected")
	}
	if err := writer.WriteSystemUserCredential(t.Context(), nil); err == nil {
		t.Fatal("nil request must be rejected")
	}
	if len(credentials.stored) != 0 {
		t.Fatalf("rejected requests must not store, got %#v", credentials.stored)
	}
}

func TestCredentialWriterPropagatesProviderFailure(t *testing.T) {
	failure := errors.New("store credential: boom")
	credentials := &recordingCredentialRepo{err: failure}
	writer := NewCredentialWriter(credentials)

	if err := writer.WriteSystemUserCredential(t.Context(), &model.SystemUserCredential{SystemUserID: 42, Password: "secret"}); !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the provider error surfaced", err)
	}
}
