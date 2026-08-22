package v1_test

import (
	"fmt"
	"strings"
	"testing"

	authv1 "glex/api/iam/auth/v1"
	menuv1 "glex/api/iam/menu/v1"
	userv1 "glex/api/iam/user/v1"

	annotations "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestAuthAndCurrentUserHTTPRoutesMatchConfirmedContract(t *testing.T) {
	type route struct {
		verb string
		path string
	}
	want := map[protoreflect.FullName]route{
		"iam.auth.v1.AuthService.Login":                          {verb: "POST", path: "/auth/login"},
		"iam.auth.v1.AuthService.Logout":                         {verb: "POST", path: "/auth/logout"},
		"iam.auth.v1.OnlineSessionService.ListOnlineSessions":    {verb: "GET", path: "/monitor/online/list"},
		"iam.auth.v1.OnlineSessionService.KickoutOnlineSession":  {verb: "DELETE", path: "/monitor/online/{token_id}"},
		"iam.auth.v1.OnlineSessionService.ListMyOnlineSessions":  {verb: "GET", path: "/monitor/online"},
		"iam.auth.v1.OnlineSessionService.DeleteMyOnlineSession": {verb: "DELETE", path: "/monitor/online/myself/{token_id}"},
		"iam.user.v1.UserService.GetInfo":                        {verb: "GET", path: "/iam/user/getInfo"},
		"iam.menu.v1.MenuService.GetRouters":                     {verb: "GET", path: "/iam/menu/getRouters"},
	}

	seen := make(map[protoreflect.FullName]bool)
	for _, method := range serviceMethods() {
		gotVerb, gotPath, err := methodHTTPRoute(method)
		if err != nil {
			t.Fatalf("%s: %v", method.FullName(), err)
		}
		expected, isContractRoute := want[method.FullName()]
		if isContractRoute {
			seen[method.FullName()] = true
			if gotVerb != expected.verb || gotPath != expected.path {
				t.Errorf("%s route = %s %s, want %s %s", method.FullName(), gotVerb, gotPath, expected.verb, expected.path)
			}
		}
		if (strings.HasPrefix(gotPath, "/auth/") || strings.HasPrefix(gotPath, "/monitor/online")) && !isContractRoute {
			t.Errorf("unexpected public auth/session route %s %s on %s", gotVerb, gotPath, method.FullName())
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("required route for %s is missing", name)
		}
	}
}

func TestManagementAndCurrentUserDTOsCannotExposeSecrets(t *testing.T) {
	tests := []struct {
		name       string
		descriptor protoreflect.MessageDescriptor
	}{
		{name: "Principal", descriptor: (&authv1.Principal{}).ProtoReflect().Descriptor()},
		{name: "OnlineSession", descriptor: (&authv1.OnlineSession{}).ProtoReflect().Descriptor()},
		{name: "CurrentUserInfo", descriptor: (&userv1.CurrentUserInfo{}).ProtoReflect().Descriptor()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNoSecretFields(t, tt.descriptor, map[protoreflect.FullName]bool{})
		})
	}
}

func assertNoSecretFields(t *testing.T, message protoreflect.MessageDescriptor, visited map[protoreflect.FullName]bool) {
	t.Helper()
	if visited[message.FullName()] {
		return
	}
	visited[message.FullName()] = true
	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		switch field.Name() {
		case "password", "password_hash", "access_token", "bearer_token", "token":
			t.Errorf("%s exposes secret field %s", message.FullName(), field.Name())
		}
		if field.Message() != nil {
			assertNoSecretFields(t, field.Message(), visited)
		}
	}
}

func serviceMethods() []protoreflect.MethodDescriptor {
	files := []protoreflect.FileDescriptor{
		authv1.File_iam_auth_v1_auth_proto,
		menuv1.File_iam_menu_v1_menu_proto,
		userv1.File_iam_user_v1_user_proto,
	}
	var result []protoreflect.MethodDescriptor
	for _, file := range files {
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
