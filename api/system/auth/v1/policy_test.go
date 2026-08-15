package v1_test

import (
	"fmt"
	"strings"
	"testing"

	authv1 "glex/api/system/auth/v1"
	policyv1 "glex/api/system/auth/v1/policy"
	menuv1 "glex/api/system/menu/v1"
	rolev1 "glex/api/system/role/v1"
	userv1 "glex/api/system/user/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestEveryRPCDeclaresAValidAuthPolicy(t *testing.T) {
	methods := serviceMethods()
	for _, method := range methods {
		if err := validateMethodPolicy(method); err != nil {
			t.Errorf("%s: %v", method.FullName(), err)
		}
	}
	if len(methods) == 0 {
		t.Fatal("no RPC descriptors were enumerated")
	}
}

func TestRPCPoliciesMatchTheConfirmedContract(t *testing.T) {
	expected := map[protoreflect.FullName]*policyv1.AuthPolicy{
		"system.auth.v1.AuthService.Login":                          &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PUBLIC},
		"system.auth.v1.AuthService.Logout":                         &policyv1.AuthPolicy{Mode: policyv1.AuthMode_OPTIONAL},
		"system.auth.v1.AuthService.ListUserRoles":                  &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:query"},
		"system.auth.v1.AuthService.UpdateUserRoles":                &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:edit"},
		"system.auth.v1.AuthService.ListRoleUsers":                  &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:list"},
		"system.auth.v1.OnlineSessionService.ListOnlineSessions":    &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "monitor:online:list"},
		"system.auth.v1.OnlineSessionService.KickoutOnlineSession":  &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "monitor:online:forceLogout"},
		"system.auth.v1.OnlineSessionService.ListMyOnlineSessions":  &policyv1.AuthPolicy{Mode: policyv1.AuthMode_AUTHENTICATED},
		"system.auth.v1.OnlineSessionService.DeleteMyOnlineSession": &policyv1.AuthPolicy{Mode: policyv1.AuthMode_AUTHENTICATED},
		"system.menu.v1.MenuService.CreateMenu":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:menu:add"},
		"system.menu.v1.MenuService.GetMenu":                        &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:menu:query"},
		"system.menu.v1.MenuService.ListMenus":                      &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:menu:list"},
		"system.menu.v1.MenuService.UpdateMenu":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:menu:edit"},
		"system.menu.v1.MenuService.DeleteMenu":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:menu:remove"},
		"system.menu.v1.MenuService.GetRouters":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_AUTHENTICATED},
		"system.role.v1.RoleService.CreateRole":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:add"},
		"system.role.v1.RoleService.GetRole":                        &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:query"},
		"system.role.v1.RoleService.ListRoles":                      &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:list"},
		"system.role.v1.RoleService.UpdateRole":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:edit"},
		"system.role.v1.RoleService.DeleteRole":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:remove"},
		"system.role.v1.RoleService.ListRoleMenus":                  &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:query"},
		"system.role.v1.RoleService.UpdateRoleMenus":                &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:role:edit"},
		"system.user.v1.UserService.CreateUser":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:add"},
		"system.user.v1.UserService.GetUser":                        &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:query"},
		"system.user.v1.UserService.ListUsers":                      &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:list"},
		"system.user.v1.UserService.UpdateUser":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:edit"},
		"system.user.v1.UserService.DeleteUser":                     &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: "system:user:remove"},
		"system.user.v1.UserService.GetInfo":                        &policyv1.AuthPolicy{Mode: policyv1.AuthMode_AUTHENTICATED},
	}

	actual := make(map[protoreflect.FullName]*policyv1.AuthPolicy)
	for _, method := range serviceMethods() {
		policy, err := methodPolicy(method)
		if err != nil {
			t.Fatalf("%s: %v", method.FullName(), err)
		}
		actual[method.FullName()] = policy
	}

	for name, want := range expected {
		got, ok := actual[name]
		if !ok {
			t.Errorf("required RPC %s is missing", name)
			continue
		}
		if got.GetMode() != want.GetMode() || got.GetPermission() != want.GetPermission() {
			t.Errorf("%s policy = {%s %q}, want {%s %q}", name, got.GetMode(), got.GetPermission(), want.GetMode(), want.GetPermission())
		}
		delete(actual, name)
	}
	for name := range actual {
		t.Errorf("RPC %s is not part of the confirmed contract", name)
	}
}

func TestDescriptorPolicyValidationRejectsMalformedDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		policy *policyv1.AuthPolicy
		want   string
	}{
		{
			name:   "unspecified mode",
			policy: &policyv1.AuthPolicy{},
			want:   "unspecified",
		},
		{
			name:   "permission without handle",
			policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION},
			want:   "no Functional Permission handle",
		},
		{
			name:   "non-permission with handle",
			policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode_AUTHENTICATED, Permission: "system:user:list"},
			want:   "must not carry",
		},
		{
			name:   "unknown mode",
			policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode(99)},
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := methodDescriptorWithPolicy(t, tt.policy)
			err := validateMethodPolicy(method)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validate malformed policy error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func methodDescriptorWithPolicy(t *testing.T, policy *policyv1.AuthPolicy) protoreflect.MethodDescriptor {
	t.Helper()
	options := &descriptorpb.MethodOptions{}
	proto.SetExtension(options, policyv1.E_AuthPolicy, policy)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("test/policy.proto"),
		Package: proto.String("test.policy"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Request")},
			{Name: proto.String("Reply")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Test"),
				InputType:  proto.String(".test.policy.Request"),
				OutputType: proto.String(".test.policy.Reply"),
				Options:    options,
			}},
		}},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("construct method descriptor: %v", err)
	}
	return file.Services().Get(0).Methods().Get(0)
}

func serviceFiles() []protoreflect.FileDescriptor {
	return []protoreflect.FileDescriptor{
		authv1.File_system_auth_v1_auth_proto,
		menuv1.File_system_menu_v1_menu_proto,
		rolev1.File_system_role_v1_role_proto,
		userv1.File_system_user_v1_user_proto,
	}
}

func serviceMethods() []protoreflect.MethodDescriptor {
	var result []protoreflect.MethodDescriptor
	for _, file := range serviceFiles() {
		services := file.Services()
		for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
			methods := services.Get(serviceIndex).Methods()
			for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
				result = append(result, methods.Get(methodIndex))
			}
		}
	}
	return result
}

func validateMethodPolicy(method protoreflect.MethodDescriptor) error {
	policy, err := methodPolicy(method)
	if err != nil {
		return err
	}
	switch policy.GetMode() {
	case policyv1.AuthMode_AUTH_MODE_UNSPECIFIED:
		return fmt.Errorf("auth policy mode is unspecified")
	case policyv1.AuthMode_PERMISSION:
		if strings.TrimSpace(policy.GetPermission()) == "" {
			return fmt.Errorf("permission policy has no Functional Permission handle")
		}
		return nil
	case policyv1.AuthMode_PUBLIC, policyv1.AuthMode_OPTIONAL, policyv1.AuthMode_AUTHENTICATED:
		if policy.GetPermission() != "" {
			return fmt.Errorf("%s policy must not carry a Functional Permission handle", policy.GetMode())
		}
		return nil
	default:
		return fmt.Errorf("auth policy mode %d is unknown", policy.GetMode())
	}
}

func methodPolicy(method protoreflect.MethodDescriptor) (*policyv1.AuthPolicy, error) {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(options, policyv1.E_AuthPolicy) {
		return nil, fmt.Errorf("auth policy is missing")
	}
	policy, ok := proto.GetExtension(options, policyv1.E_AuthPolicy).(*policyv1.AuthPolicy)
	if !ok || policy == nil {
		return nil, fmt.Errorf("auth policy has an invalid type")
	}
	return policy, nil
}
