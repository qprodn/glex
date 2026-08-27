package service

import (
	"context"
	"testing"

	"glex/internal/domain/authn/entity"
)

func TestPrincipalContextRoundTrip(t *testing.T) {
	principal := entity.NewPrincipal(42, "alice", "session-id", nil, nil)
	ctx := WithPrincipal(context.Background(), principal)
	got, ok := PrincipalFromContext(ctx)
	if !ok || got != principal {
		t.Fatalf("PrincipalFromContext = %#v/%v", got, ok)
	}
	if _, ok := PrincipalFromContext(context.Background()); ok {
		t.Fatal("empty context unexpectedly contains a Principal")
	}
}
