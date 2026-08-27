package authn

import (
	"context"
	"testing"

	authnrepo "glex/internal/domain/authn/repository"
)

// fakeTransaction runs fn inline and returns the recorded body error, or —
// after a successful body — err, simulating a commit failure that happens
// after every repo call and Redis write inside the closure.
type fakeTransaction struct {
	err error
}

func (m *fakeTransaction) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := fn(ctx); err != nil {
		return err
	}
	return m.err
}

func newFakeTransaction() *fakeTransaction {
	return &fakeTransaction{}
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

// stubSessionRepo satisfies LoginSessionRepo for flows that never touch the
// session store directly; the embedded interface stays nil.
type stubSessionRepo struct {
	authnrepo.LoginSessionRepo
}
