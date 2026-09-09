package v1_test

import (
	"fmt"
	"testing"

	userv1 "glex/api/user/v1"

	annotations "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestUserHTTPRoutesMatchConfirmedContract(t *testing.T) {
	type route struct {
		verb string
		path string
	}
	want := map[protoreflect.FullName]route{
		"user.v1.UserService.GetInfo": {verb: "GET", path: "/iam/user/getInfo"},
	}
	// handWritten declares the deliberately annotation-less methods (ADR
	// 0017): streaming byte responses the proto generators cannot express
	// (the file module's transfer precedent). The routes live in
	// internal/server/user_excel.go and must keep these shapes — the entry
	// documents the pairing the proto cannot.
	handWritten := map[protoreflect.FullName]route{
		"user.v1.UserService.ExportUsers":    {verb: "POST", path: "/iam/user/-/export"},
		"user.v1.UserService.ImportTemplate": {verb: "POST", path: "/iam/user/-/importTemplate"},
	}

	seen := make(map[protoreflect.FullName]bool)
	services := userv1.File_user_v1_user_proto.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		methods := services.Get(serviceIndex).Methods()
		for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
			method := methods.Get(methodIndex)
			gotVerb, gotPath, err := methodHTTPRoute(method)
			if err != nil {
				if _, deliberate := handWritten[method.FullName()]; deliberate {
					seen[method.FullName()] = true
					continue
				}
				t.Fatalf("%s: %v", method.FullName(), err)
			}
			if expected, ok := want[method.FullName()]; ok {
				seen[method.FullName()] = true
				if gotVerb != expected.verb || gotPath != expected.path {
					t.Errorf("%s route = %s %s, want %s %s", method.FullName(), gotVerb, gotPath, expected.verb, expected.path)
				}
			}
			if _, duplicate := handWritten[method.FullName()]; duplicate && err == nil {
				t.Errorf("%s is declared hand-written but carries an annotation; drop one of the two", method.FullName())
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("required route for %s is missing", name)
		}
	}
	for name := range handWritten {
		if !seen[name] {
			t.Errorf("hand-written route declaration for %s is stale: the method no longer exists", name)
		}
	}
}

func TestCurrentUserInfoCannotExposeSecrets(t *testing.T) {
	assertNoSecretFields(t, (&userv1.CurrentUserInfo{}).ProtoReflect().Descriptor(), map[protoreflect.FullName]bool{})
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
