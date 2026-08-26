package user

import (
	"context"
	"testing"

	authnmodel "glex/internal/crossdomain/authn/model"
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

// fakeSessionRevoker records each revocation request against the authn
// crossdomain contract.
type fakeSessionRevoker struct {
	requests      []*authnmodel.RevocationRequest
	err           error
	failOnRequest int // 1-based; when >0, the nth request fails
}

func (r *fakeSessionRevoker) RevokeSystemUsers(_ context.Context, request *authnmodel.RevocationRequest) error {
	snapshot := *request
	snapshot.SystemUserIDs = append([]int64(nil), request.SystemUserIDs...)
	r.requests = append(r.requests, &snapshot)
	if r.failOnRequest > 0 && len(r.requests) == r.failOnRequest && r.err != nil {
		return r.err
	}
	return nil
}
