package user

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	userentity "glex/internal/domain/user/entity"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGetInfoNeverExposesPassword(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newFakeUserRepo()
	repo.byID[42] = &userentity.User{
		ID: 42, UserName: "alice", NickName: "Alice", Password: "bcrypt-secret-hash",
		Email: "alice@example.com", LoginDate: &now, CreatedAt: now, UpdatedAt: now,
	}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(
		42, "alice", "safe-session-id", []string{"operator"}, []string{"system:user:list"},
	))

	reply, err := app.GetInfo(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if reply.GetUser().GetUserId() != 42 || reply.GetUser().GetUserName() != "alice" {
		t.Fatalf("current System User = %#v", reply.GetUser())
	}
	if strings.Join(reply.GetRoleKeys(), ",") != "operator" || strings.Join(reply.GetPermissions(), ",") != "system:user:list" {
		t.Fatalf("snapshot projection = Roles %v, Functional Permissions %v", reply.GetRoleKeys(), reply.GetPermissions())
	}
	encoded, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("marshal GetInfo: %v", err)
	}
	if strings.Contains(string(encoded), "bcrypt-secret-hash") || strings.Contains(string(encoded), "password") {
		t.Fatalf("GetInfo exposed password data: %s", encoded)
	}
}

func TestCurrentUserHandlersRequirePrincipal(t *testing.T) {
	app := newTestUserApplication(newFakeUserRepo(), new(fakeCredentialWriter), new(fakeSessionRevoker))
	if _, err := app.GetInfo(context.Background(), &emptypb.Empty{}); err != authndomain.ErrCredentialMissing {
		t.Fatalf("GetInfo without Principal = %v", err)
	}
}
