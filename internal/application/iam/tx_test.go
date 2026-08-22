package iam

import (
	. "glex/internal/domain/iam"

	"context"
	"testing"
)

// fakeTransaction runs fn inline and returns the recorded body error, or —
// after a successful body — err, simulating a commit failure that happens
// after every repo call and Redis write inside the closure.
type fakeTransaction struct {
	err   error
	steps *[]string
	began int
}

func (m *fakeTransaction) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	m.began++
	if err := fn(ctx); err != nil {
		return err
	}
	return m.err
}

func newFakeTransaction() *fakeTransaction {
	return &fakeTransaction{}
}

// step records an ordered label shared between the transaction fake and the
// revoker fake, so tests can assert the revocation is the last step inside
// the closure.
func (m *fakeTransaction) step() *[]string {
	m.steps = &[]string{}
	return m.steps
}

func recordStep(steps *[]string, label string) {
	if steps != nil {
		*steps = append(*steps, label)
	}
}

func assertSteps(t *testing.T, steps *[]string, want ...string) {
	t.Helper()
	if len(*steps) != len(want) {
		t.Fatalf("steps = %v, want %v", *steps, want)
	}
	for i, label := range want {
		if (*steps)[i] != label {
			t.Fatalf("steps = %v, want %v", *steps, want)
		}
	}
}

// fakeSessionRevoker records each revocation batch.
type fakeSessionRevoker struct {
	batches     [][]SystemUserSessionRevocation
	err         error
	failOnBatch int // 1-based; when >0, the nth batch fails
}

func (r *fakeSessionRevoker) RevokeSystemUsers(_ context.Context, revocations []SystemUserSessionRevocation) error {
	batch := append([]SystemUserSessionRevocation(nil), revocations...)
	r.batches = append(r.batches, batch)
	if r.failOnBatch > 0 && len(r.batches) == r.failOnBatch && r.err != nil {
		return r.err
	}
	return nil
}

// recordingSessionRepo is a LoginSessionRepo whose only live method is
// RevokeSystemUsers; the embedded interface stays nil because the flows under
// test never call the rest.
type recordingSessionRepo struct {
	LoginSessionRepo
	revocations []SystemUserSessionRevocation
	err         error
}

func (r *recordingSessionRepo) RevokeSystemUsers(_ context.Context, revocations []SystemUserSessionRevocation) error {
	r.revocations = append(r.revocations, revocations...)
	return r.err
}

func revokedUserIDs(revocations []SystemUserSessionRevocation) []int64 {
	ids := make([]int64, 0, len(revocations))
	for _, revocation := range revocations {
		ids = append(ids, revocation.SystemUserID)
	}
	return ids
}
