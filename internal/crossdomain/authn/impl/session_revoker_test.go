package impl

import (
	"context"
	"testing"

	"glex/internal/crossdomain/authn/model"
	iam "glex/internal/domain/iam"
)

// recordingLoginSessionRepo records batch revocations; the embedded interface
// stays nil because the adapter under test touches nothing else.
type recordingLoginSessionRepo struct {
	iam.LoginSessionRepo
	batches [][]iam.SystemUserSessionRevocation
	err     error
}

func (r *recordingLoginSessionRepo) RevokeSystemUsers(_ context.Context, batch []iam.SystemUserSessionRevocation) error {
	r.batches = append(r.batches, append([]iam.SystemUserSessionRevocation(nil), batch...))
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
	for _, revocation := range sessions.batches[0] {
		if !revocation.Policy.Required() || revocation.Policy.PreservedSessionID() != "" {
			t.Fatalf("revocation = {%d, %#v}, want revoke-all", revocation.SystemUserID, revocation.Policy)
		}
	}
	if sessions.batches[0][0].SystemUserID != 7 || sessions.batches[0][1].SystemUserID != 42 {
		t.Fatalf("batch = %#v, want affected users in request order", sessions.batches[0])
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
	revocation := sessions.batches[0][0]
	if revocation.SystemUserID != 42 || revocation.Policy.PreservedSessionID() != "current-session" {
		t.Fatalf("revocation = {%d, %#v}, want {42, preserve current-session}", revocation.SystemUserID, revocation.Policy)
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
	sessions := &recordingLoginSessionRepo{err: iam.ErrAuthBackendUnavailable}
	revoker := NewSessionRevoker(sessions)

	if err := revoker.RevokeSystemUsers(t.Context(), &model.RevocationRequest{SystemUserIDs: []int64{42}}); err != iam.ErrAuthBackendUnavailable {
		t.Fatalf("error = %v, want the provider error surfaced unchanged", err)
	}
}
