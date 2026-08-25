package access

import (
	"context"
	"testing"
)

func TestRevokeAllSystemUsersAssemblesSortedDedupedRequest(t *testing.T) {
	revoker := &fakeSessionRevoker{}

	if err := revokeAllSystemUsers(context.Background(), revoker, 9, 0, 4, 4, 2); err != nil {
		t.Fatalf("revokeAllSystemUsers: %v", err)
	}
	if len(revoker.requests) != 1 {
		t.Fatalf("requests = %#v, want exactly one", revoker.requests)
	}
	request := revoker.requests[0]
	want := []int64{2, 4, 9}
	if len(request.SystemUserIDs) != len(want) || request.SystemUserIDs[0] != want[0] || request.SystemUserIDs[1] != want[1] || request.SystemUserIDs[2] != want[2] {
		t.Fatalf("SystemUserIDs = %v, want %v sorted and deduped (invalid ids dropped)", request.SystemUserIDs, want)
	}
	if request.PreservedSessionID != "" {
		t.Fatalf("PreservedSessionID = %q, want empty (access writes are administrative)", request.PreservedSessionID)
	}
}

func TestRevokeAllSystemUsersSkipsWhenNoValidIDs(t *testing.T) {
	revoker := &fakeSessionRevoker{}
	if err := revokeAllSystemUsers(context.Background(), revoker, 0, -1); err != nil {
		t.Fatalf("revokeAllSystemUsers: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("requests = %#v, want none", revoker.requests)
	}
}
