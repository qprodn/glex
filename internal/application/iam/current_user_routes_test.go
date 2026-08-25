package iam

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	authndomain "glex/internal/domain/authn"
	iamdomain "glex/internal/domain/iam"

	"google.golang.org/protobuf/types/known/emptypb"
)

type currentUserUsecase struct {
	userUsecase
	info      *iamdomain.CurrentUserInfo
	principal *authndomain.Principal
}

func (u *currentUserUsecase) GetCurrentUserInfo(_ context.Context, principal *authndomain.Principal) (*iamdomain.CurrentUserInfo, error) {
	u.principal = principal
	return u.info, nil
}

func TestGetInfoUsesPrincipalSnapshotAndNeverExposesPassword(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	uc := &currentUserUsecase{info: &iamdomain.CurrentUserInfo{
		User: &iamdomain.User{
			ID: 42, UserName: "alice", NickName: "Alice", Password: "bcrypt-secret-hash",
			Email: "alice@example.com", LoginDate: &now, CreatedAt: now, UpdatedAt: now,
		},
		RoleKeys: []string{"operator"}, Permissions: []string{"system:user:list"},
	}}
	service := newUserApplication(uc, testLogger())
	ctx := authndomain.WithPrincipal(t.Context(), authndomain.NewPrincipal(
		42, "alice", "safe-session-id", []string{"operator"}, []string{"system:user:list"},
	))

	reply, err := service.GetInfo(ctx, &emptypb.Empty{})
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
	if uc.principal == nil || uc.principal.SystemUserID() != 42 {
		t.Fatalf("GetInfo did not pass Principal to usecase: %#v", uc.principal)
	}
}

func TestCurrentUserHandlersRequirePrincipal(t *testing.T) {
	userService := newUserApplication(&currentUserUsecase{}, testLogger())
	if _, err := userService.GetInfo(t.Context(), &emptypb.Empty{}); err != authndomain.ErrCredentialMissing {
		t.Fatalf("GetInfo without Principal = %v", err)
	}
}
