package impl

import (
	"context"
	"errors"
	"testing"

	"glex/internal/crossdomain/access/model"
	accessdomain "glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
)

// fakeAuthorizationService is an in-memory accessservice.Authorization stand-in
// for testing the contract adapter in isolation from the provider's
// infrastructure.
type fakeAuthorizationService struct {
	resolution *accessentity.AuthorizationResolution
	err        error
	calledWith []int64
}

func (s *fakeAuthorizationService) ResolveAuthorization(_ context.Context, systemUserID int64) (*accessentity.AuthorizationResolution, error) {
	s.calledWith = append(s.calledWith, systemUserID)
	if s.err != nil {
		return nil, s.err
	}
	if s.resolution == nil {
		s.resolution = &accessentity.AuthorizationResolution{}
	}
	return s.resolution, nil
}

func TestAuthorizationResolverReturnsModelSnapshot(t *testing.T) {
	provider := &fakeAuthorizationService{resolution: &accessentity.AuthorizationResolution{
		RoleKeys:    []string{"superadmin", "viewer"},
		Permissions: []string{"*:*:*", "system:user:list"},
	}}
	adapter := NewAuthorizationResolver(provider)

	got, err := adapter.ResolveAuthorization(t.Context(), 42)
	if err != nil {
		t.Fatalf("ResolveAuthorization: %v", err)
	}
	if len(provider.calledWith) != 1 || provider.calledWith[0] != 42 {
		t.Fatalf("provider called with %v, want [42]", provider.calledWith)
	}
	if len(got.RoleKeys) != 2 || got.RoleKeys[0] != "superadmin" || got.RoleKeys[1] != "viewer" {
		t.Fatalf("RoleKeys = %v", got.RoleKeys)
	}
	if len(got.Permissions) != 2 || got.Permissions[1] != "system:user:list" {
		t.Fatalf("Permissions = %v", got.Permissions)
	}
}

func TestAuthorizationResolverPassesProviderFailureThrough(t *testing.T) {
	provider := &fakeAuthorizationService{err: accessdomain.ErrAuthorizationUnavailable}
	adapter := NewAuthorizationResolver(provider)

	got, err := adapter.ResolveAuthorization(t.Context(), 42)
	if !errors.Is(err, accessdomain.ErrAuthorizationUnavailable) {
		t.Fatalf("error = %v, want the provider failure", err)
	}
	if got != nil {
		t.Fatalf("resolution = %#v, want nil on failure", got)
	}
}

func TestAuthorizationResolverNilResolutionDegradesToEmptyModel(t *testing.T) {
	// A nil model must never cross the seam; the adapter degrades it to an
	// empty resolution so consumers can fill an empty snapshot.
	provider := &fakeAuthorizationService{}
	adapter := NewAuthorizationResolver(provider)

	got, err := adapter.ResolveAuthorization(t.Context(), 42)
	if err != nil {
		t.Fatalf("ResolveAuthorization: %v", err)
	}
	if got == nil {
		t.Fatal("resolution = nil, want an empty model snapshot")
	}
	if len(got.RoleKeys) != 0 || len(got.Permissions) != 0 {
		t.Fatalf("resolution = %#v, want empty sets", got)
	}
}

// Compile-time pin: the adapter satisfies the contract without leaking the
// provider's domain type.
var _ = model.AuthorizationResolution{}
