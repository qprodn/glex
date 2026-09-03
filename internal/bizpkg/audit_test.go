package bizpkg

import (
	"context"
	"testing"

	"glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
)

func TestAuditActorReturnsPrincipalID(t *testing.T) {
	principal := entity.NewPrincipal(42, "alice", "session-id", nil, nil)
	ctx := authnservice.WithPrincipal(context.Background(), principal)
	if got := AuditActor(ctx); got != 42 {
		t.Fatalf("AuditActor = %d, want 42", got)
	}
}

func TestAuditActorDefaultsToSystem(t *testing.T) {
	if got := AuditActor(context.Background()); got != 0 {
		t.Fatalf("AuditActor = %d, want 0", got)
	}
}
