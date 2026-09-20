package service

import (
	"context"
	"testing"

	"glex/internal/domain/authn/entity"
)

func TestPrincipalContextRoundTrip(t *testing.T) {
	principal := entity.NewPrincipal(42, "alice", "session-id", nil, nil)
	ctx := WithPrincipal(context.Background(), principal)
	if got := PrincipalFromContext(ctx); got != principal {
		t.Fatalf("PrincipalFromContext = %#v, want the attached Principal", got)
	}
	if PrincipalFromContext(context.Background()) != nil {
		t.Fatal("empty context unexpectedly contains a Principal")
	}
}
