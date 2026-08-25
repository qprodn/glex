package iam

import (
	"context"
	"testing"

	. "glex/internal/domain/iam"
)

func TestRevokeSystemUsersAssemblesSortedDedupedRequest(t *testing.T) {
	revoker := &fakeSessionRevoker{}
	revocations := []SystemUserSessionRevocation{
		{SystemUserID: 9, Policy: RevokeAllLoginSessions()},
		{SystemUserID: 0, Policy: RevokeAllLoginSessions()}, // invalid id: dropped
		{SystemUserID: 4, Policy: SessionRevocation{}},      // not required: dropped
		{SystemUserID: 4, Policy: RevokeAllLoginSessions()}, // duplicate user: kept
		{SystemUserID: 2, Policy: RevokeAllLoginSessions()},
	}

	if err := revokeSystemUsers(context.Background(), revoker, revocations...); err != nil {
		t.Fatalf("revokeSystemUsers: %v", err)
	}
	want := []int64{2, 4, 9}
	if len(revoker.requests) != 1 {
		t.Fatalf("requests = %#v, want exactly one", revoker.requests)
	}
	request := revoker.requests[0]
	if len(request.SystemUserIDs) != len(want) || request.SystemUserIDs[0] != want[0] || request.SystemUserIDs[1] != want[1] || request.SystemUserIDs[2] != want[2] {
		t.Fatalf("SystemUserIDs = %v, want %v sorted and deduped", request.SystemUserIDs, want)
	}
	if request.PreservedSessionID != "" {
		t.Fatalf("PreservedSessionID = %q, want empty", request.PreservedSessionID)
	}
}

func TestRevokeSystemUsersSkipsWhenNothingRequired(t *testing.T) {
	revoker := &fakeSessionRevoker{}
	if err := revokeSystemUsers(context.Background(), revoker,
		SystemUserSessionRevocation{SystemUserID: 4, Policy: SessionRevocation{}},
		SystemUserSessionRevocation{SystemUserID: 0, Policy: RevokeAllLoginSessions()},
	); err != nil {
		t.Fatalf("revokeSystemUsers: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("requests = %#v, want none", revoker.requests)
	}
}

func TestRevokeSystemUsersPreservesTheClassifiedSession(t *testing.T) {
	revoker := &fakeSessionRevoker{}
	if err := revokeSystemUsers(context.Background(), revoker,
		SystemUserSessionRevocation{SystemUserID: 42, Policy: PreserveLoginSession("current-session")},
	); err != nil {
		t.Fatalf("revokeSystemUsers: %v", err)
	}
	if len(revoker.requests) != 1 {
		t.Fatalf("requests = %#v, want exactly one", revoker.requests)
	}
	request := revoker.requests[0]
	if len(request.SystemUserIDs) != 1 || request.SystemUserIDs[0] != 42 || request.PreservedSessionID != "current-session" {
		t.Fatalf("request = %#v, want {42, preserve current-session}", request)
	}
}

func TestRevokeSystemUsersRejectsConflictingPreservedSessions(t *testing.T) {
	// The flat request carries one preserved session; a batch classifying two
	// distinct preserves is a programming error that must fail loudly instead
	// of nondeterministically revoking one user's current session.
	revoker := &fakeSessionRevoker{}
	err := revokeSystemUsers(context.Background(), revoker,
		SystemUserSessionRevocation{SystemUserID: 1, Policy: PreserveLoginSession("session-a")},
		SystemUserSessionRevocation{SystemUserID: 2, Policy: PreserveLoginSession("session-b")},
	)
	if err == nil {
		t.Fatal("conflicting preserved sessions must be rejected")
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("requests = %#v, want none on conflict", revoker.requests)
	}
}
