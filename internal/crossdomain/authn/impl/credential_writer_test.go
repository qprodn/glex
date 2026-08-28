package impl

import (
	"context"
	"errors"
	"testing"

	"glex/internal/crossdomain/authn/model"
)

// recordingCredentialService records Store calls; hashing is the provider's
// domain policy, so the adapter must hand over the plaintext verbatim.
type recordingCredentialService struct {
	stored map[int64]string
	err    error
}

func (s *recordingCredentialService) Store(_ context.Context, systemUserID int64, password string) error {
	if s.stored == nil {
		s.stored = make(map[int64]string)
	}
	s.stored[systemUserID] = password
	return s.err
}

func TestCredentialWriterPassesPlaintextToTheProvider(t *testing.T) {
	credentials := &recordingCredentialService{}
	writer := NewCredentialWriter(credentials)

	if err := writer.WriteSystemUserCredential(t.Context(), &model.SystemUserCredential{SystemUserID: 42, Password: "secret"}); err != nil {
		t.Fatalf("WriteSystemUserCredential: %v", err)
	}
	stored, ok := credentials.stored[42]
	if !ok {
		t.Fatal("no credential was delegated to the provider")
	}
	if stored != "secret" {
		t.Fatalf("delegated password = %q, want the plaintext (hashing is the provider's policy)", stored)
	}
}

func TestCredentialWriterRejectsMissingUser(t *testing.T) {
	credentials := &recordingCredentialService{}
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
	credentials := &recordingCredentialService{err: failure}
	writer := NewCredentialWriter(credentials)

	if err := writer.WriteSystemUserCredential(t.Context(), &model.SystemUserCredential{SystemUserID: 42, Password: "secret"}); !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the provider error surfaced", err)
	}
}
