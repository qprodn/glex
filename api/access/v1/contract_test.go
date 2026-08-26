package v1_test

import (
	"fmt"
	"testing"

	accessv1 "glex/api/access/v1"

	annotations "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestAccessHTTPRoutesMatchConfirmedContract(t *testing.T) {
	type route struct {
		verb string
		path string
	}
	want := map[protoreflect.FullName]route{
		"access.v1.MenuService.GetRouters":                {verb: "GET", path: "/iam/menu/getRouters"},
		"access.v1.RoleAssignmentService.ListUserRoles":   {verb: "GET", path: "/iam/user/{user_id}/roles"},
		"access.v1.RoleAssignmentService.UpdateUserRoles": {verb: "PUT", path: "/iam/user/{user_id}/roles"},
		"access.v1.RoleAssignmentService.ListRoleUsers":   {verb: "GET", path: "/iam/role/{role_id}/users"},
	}

	seen := make(map[protoreflect.FullName]bool)
	files := []protoreflect.FileDescriptor{
		accessv1.File_access_v1_role_proto,
		accessv1.File_access_v1_menu_proto,
		accessv1.File_access_v1_role_assignment_proto,
	}
	for _, file := range files {
		services := file.Services()
		for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
			methods := services.Get(serviceIndex).Methods()
			for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
				method := methods.Get(methodIndex)
				gotVerb, gotPath, err := methodHTTPRoute(method)
				if err != nil {
					t.Fatalf("%s: %v", method.FullName(), err)
				}
				expected, ok := want[method.FullName()]
				if ok {
					seen[method.FullName()] = true
					if gotVerb != expected.verb || gotPath != expected.path {
						t.Errorf("%s route = %s %s, want %s %s", method.FullName(), gotVerb, gotPath, expected.verb, expected.path)
					}
				}
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("required route for %s is missing", name)
		}
	}
}

// The descriptor helpers below are deliberately local: each module tree's
// contract test stays self-contained so a future service extraction can take
// it along unchanged.

func methodHTTPRoute(method protoreflect.MethodDescriptor) (verb string, path string, err error) {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(options, annotations.E_Http) {
		return "", "", fmt.Errorf("HTTP binding is missing")
	}
	rule, ok := proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
	if !ok || rule == nil {
		return "", "", fmt.Errorf("HTTP binding has an invalid type")
	}
	switch pattern := rule.Pattern.(type) {
	case *annotations.HttpRule_Get:
		return "GET", pattern.Get, nil
	case *annotations.HttpRule_Post:
		return "POST", pattern.Post, nil
	case *annotations.HttpRule_Delete:
		return "DELETE", pattern.Delete, nil
	case *annotations.HttpRule_Put:
		return "PUT", pattern.Put, nil
	case *annotations.HttpRule_Patch:
		return "PATCH", pattern.Patch, nil
	default:
		return "", "", fmt.Errorf("unsupported HTTP binding")
	}
}
