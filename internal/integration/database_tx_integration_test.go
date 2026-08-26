package integration

import (
	"context"
	"errors"
	"testing"

	"glex/internal/infra/ent/user"
)

// The tests in this file exercise the generated Database wrapper's InTx
// against real PostgreSQL: rollback on closure error, nested-join semantics,
// and ctx-resolved writes staying invisible outside the transaction until
// commit. They share the integration-store gating (GLEX_TEST_POSTGRES_DSN /
// GLEX_TEST_REDIS_ADDR) because one graph wires both clients.

func TestDatabaseTxRollsBackOnClosureErrorIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)
	rollbackErr := errors.New("injected closure error")

	err := h.graph.tx.InTx(t.Context(), func(ctx context.Context) error {
		if _, err := h.data.DB.User(ctx).UpdateOneID(uid).SetNickName("rolled-back").Save(ctx); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("InTx error = %v, want the closure error", err)
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil {
		t.Fatalf("re-read user: %v", err)
	}
	if got.NickName == "rolled-back" {
		t.Fatal("mutation survived a rolled-back transaction")
	}
}

func TestDatabaseTxNestedInTxJoinsTheOuterTransactionIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)

	err := h.graph.tx.InTx(t.Context(), func(ctx context.Context) error {
		// The inner InTx joins the outer transaction (no second BEGIN), so
		// its write commits with the outer closure — and would roll back
		// with it too.
		return h.graph.tx.InTx(ctx, func(ctx context.Context) error {
			_, err := h.data.DB.User(ctx).UpdateOneID(uid).SetNickName("joined").Save(ctx)
			return err
		})
	})
	if err != nil {
		t.Fatalf("nested InTx: %v", err)
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil {
		t.Fatalf("re-read user: %v", err)
	}
	if got.NickName != "joined" {
		t.Fatalf("nested write did not commit with the outer transaction: %q", got.NickName)
	}

	rollbackErr := errors.New("outer rollback discards the joined write")
	err = h.graph.tx.InTx(t.Context(), func(ctx context.Context) error {
		if err := h.graph.tx.InTx(ctx, func(ctx context.Context) error {
			_, err := h.data.DB.User(ctx).UpdateOneID(uid).SetNickName("joined-rolled-back").Save(ctx)
			return err
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("outer InTx error = %v, want the injected error", err)
	}
	got, err = h.client.User.Get(t.Context(), uid)
	if err != nil {
		t.Fatalf("re-read user: %v", err)
	}
	if got.NickName != "joined" {
		t.Fatalf("outer rollback must discard the inner joined write, got %q", got.NickName)
	}
}

func TestDatabaseTxWritesAreInvisibleUntilCommitIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)

	committed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- h.graph.tx.InTx(t.Context(), func(ctx context.Context) error {
			if _, err := h.data.DB.User(ctx).UpdateOneID(uid).SetNickName("uncommitted").Save(ctx); err != nil {
				return err
			}
			// The base client (outside the transaction) must still see the
			// old value while this closure runs.
			fresh, err := h.client.User.Query().Where(user.IDEQ(uid)).Only(t.Context())
			if err != nil {
				return err
			}
			if fresh.NickName == "uncommitted" {
				return errors.New("uncommitted write visible to the base client")
			}
			close(committed)
			return nil
		})
	}()

	var txErr error
	select {
	case <-committed:
	case txErr = <-done:
		t.Fatalf("InTx inside the transaction failed: %v", txErr)
	}
	if err := <-done; err != nil {
		t.Fatalf("InTx: %v", err)
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil {
		t.Fatalf("re-read user: %v", err)
	}
	if got.NickName != "uncommitted" {
		t.Fatalf("committed write missing after InTx returned: %q", got.NickName)
	}
}

func TestDatabaseTxRunsStandaloneWithoutOuterTransactionIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)

	if err := h.graph.tx.InTx(t.Context(), func(ctx context.Context) error {
		_, err := h.data.DB.User(ctx).UpdateOneID(uid).SetNickName("standalone").Save(ctx)
		return err
	}); err != nil {
		t.Fatalf("standalone InTx: %v", err)
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil {
		t.Fatalf("re-read user: %v", err)
	}
	if got.NickName != "standalone" {
		t.Fatalf("standalone InTx did not commit: %q", got.NickName)
	}
}
