package impl

import (
	"context"
	"testing"

	"glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
)

// recordingLoginSessionRepo records batch revocations; the embedded interface
// stays nil because the adapter under test touches nothing else.
type recordingLoginSessionRepo struct {
	authndomain.LoginSessionRepo
	batches [][]int64
	excepts []string
	err     error
}

func (r *recordingLoginSessionRepo) RevokeSystemUsers(_ context.Context, systemUserIDs []int64, exceptSessionID string) error {
	r.batches = append(r.batches, append([]int64(nil), systemUserIDs...))
	r.excepts = append(r.excepts, exceptSessionID)
	return r.err
}

func TestSessionRevokerRevokeAllForEveryAffectedUser(t *testing.T) {
	sessions := &recordingLoginSessionRepo{}
	revoker := NewSessionRevoker(sessions)

	err := revoker.RevokeSystemUsers(t.Context(), &model.RevocationRequest{SystemUserIDs: []int64{7, 42}})
	if err != nil {
		t.Fatalf("RevokeSystemUsers: %v", err)
	}
	if len(sessions.batches) != 1 || len(sessions.batches[0]) != 2 {
		t.Fatalf("batches = %#v, want one batch of two revocations", sessions.batches)
	}
	if sessions.batches[0][0] != 7 || sessions.batches[0][1] != 42 {
		t.Fatalf("batch = %v, want affected users in request order", sessions.batches[0])
	}
	if sessions.excepts[0] != "" {
		t.Fatalf("except = %q, want revoke-all", sessions.excepts[0])
	}
}

func TestSessionRevokerPreservesRequestedSession(t *testing.T) {
	sessions := &recordingLoginSessionRepo{}
	revoker := NewSessionRevoker(sessions)

	err := revoker.RevokeSystemUsers(t.Context(), &model.RevocationRequest{
		SystemUserIDs:      []int64{42},
		PreservedSessionID: "current-session",
	})
	if err != nil {
		t.Fatalf("RevokeSystemUsers: %v", err)
	}
	if len(sessions.batches) != 1 || len(sessions.batches[0]) != 1 {
		t.Fatalf("batches = %#v, want one single-user batch", sessions.batches)
	}
	if sessions.batches[0][0] != 42 || sessions.excepts[0] != "current-session" {
		t.Fatalf("revocation = {%d, %q}, want {42, preserve current-session}", sessions.batches[0][0], sessions.excepts[0])
	}
}

func TestSessionRevokerSkipsEmptyRequest(t *testing.T) {
	sessions := &recordingLoginSessionRepo{}
	revoker := NewSessionRevoker(sessions)

	if err := revoker.RevokeSystemUsers(t.Context(), nil); err != nil {
		t.Fatalf("nil request: %v", err)
	}
	if err := revoker.RevokeSystemUsers(t.Context(), &model.RevocationRequest{}); err != nil {
		t.Fatalf("empty request: %v", err)
	}
	if len(sessions.batches) != 0 {
		t.Fatalf("empty requests must not touch the provider, got %#v", sessions.batches)
	}
}

func TestSessionRevokerPropagatesProviderFailure(t *testing.T) {
	sessions := &recordingLoginSessionRepo{err: authndomain.ErrAuthBackendUnavailable}
	revoker := NewSessionRevoker(sessions)

	if err := revoker.RevokeSystemUsers(t.Context(), &model.RevocationRequest{SystemUserIDs: []int64{42}}); err != authndomain.ErrAuthBackendUnavailable {
		t.Fatalf("error = %v, want the provider error surfaced unchanged", err)
	}
}
