package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"glex/internal/domain/authn/repository"
)

// fakeCredentialRepo records stored hashes by System User.
type fakeCredentialRepo struct {
	stored map[int64]string
	err    error
}

func (r *fakeCredentialRepo) StoreCredential(_ context.Context, req repository.StoreCredentialRequest) error {
	if r.stored == nil {
		r.stored = make(map[int64]string)
	}
	r.stored[req.SystemUserID] = req.PasswordHash
	return r.err
}

func TestCredentialStoreHashesThePlaintext(t *testing.T) {
	credentials := &fakeCredentialRepo{}

	if err := NewCredentialService(credentials, testLogger()).Store(t.Context(), StoreRequest{SystemUserID: 42, Password: "secret"}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	stored, ok := credentials.stored[42]
	if !ok {
		t.Fatal("no credential was stored")
	}
	if stored == "secret" || !strings.HasPrefix(stored, "$2") {
		t.Fatalf("stored credential = %q, want a bcrypt hash, not the plaintext", stored)
	}
	if !VerifyPassword(stored, "secret") {
		t.Fatal("stored credential does not verify against the plaintext")
	}
}

func TestCredentialStorePropagatesBackendFailure(t *testing.T) {
	failure := errors.New("store credential: boom")
	credentials := &fakeCredentialRepo{err: failure}

	if err := NewCredentialService(credentials, testLogger()).Store(t.Context(), StoreRequest{SystemUserID: 42, Password: "secret"}); !errors.Is(err, failure) {
		t.Fatalf("Store error = %v, want the backend failure surfaced", err)
	}
}
