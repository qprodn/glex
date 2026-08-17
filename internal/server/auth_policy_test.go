package server

import (
	"strings"
	"testing"

	policyv1 "glex/api/system/auth/v1/policy"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestProjectAuthPoliciesAreCompleteAtStartup(t *testing.T) {
	registrations := httpServiceRegistrations(nil, nil, nil, nil, nil)
	descriptors := make([]protoreflect.ServiceDescriptor, len(registrations))
	for i, registration := range registrations {
		descriptors[i] = registration.descriptor
	}
	registry, err := newAuthPolicyRegistry(descriptors...)
	if err != nil {
		t.Fatalf("newProjectAuthPolicyRegistry: %v", err)
	}
	if len(registry.byOperation) == 0 {
		t.Fatal("project policy registry is empty")
	}
	wantOperations := 0
	for _, descriptor := range descriptors {
		wantOperations += descriptor.Methods().Len()
	}
	if len(registry.byOperation) != wantOperations {
		t.Fatalf("registered policy operations = %d, want exactly %d registered RPCs", len(registry.byOperation), wantOperations)
	}
}

func TestStartupPolicyValidationRejectsMissingAndMalformedPolicies(t *testing.T) {
	tests := []struct {
		name       string
		methodName string
		policy     *policyv1.AuthPolicy
		want       string
	}{
		{name: "missing", methodName: "Test", want: "missing"},
		{name: "unspecified", methodName: "Test", policy: &policyv1.AuthPolicy{}, want: "unspecified"},
		{name: "permission handle missing", methodName: "Test", policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION}, want: "no Functional Permission"},
		{name: "permission surrounding whitespace", methodName: "Test", policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PERMISSION, Permission: " system:user:list "}, want: "surrounding whitespace"},
		{name: "public with permission", methodName: "Test", policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode_PUBLIC, Permission: "system:user:list"}, want: "must not carry"},
		{name: "optional outside logout", methodName: "Test", policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode_OPTIONAL}, want: "reserved for Logout"},
		{name: "unknown mode", methodName: "Test", policy: &policyv1.AuthPolicy{Mode: policyv1.AuthMode(99)}, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := policyTestFile(t, tt.methodName, tt.policy).Services().Get(0)
			_, err := newAuthPolicyRegistry(service)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("startup validation error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPolicyRegistryUsesGeneratedTransportOperationKey(t *testing.T) {
	service := policyTestFile(t, "Test", &policyv1.AuthPolicy{Mode: policyv1.AuthMode_AUTHENTICATED}).Services().Get(0)
	registry, err := newAuthPolicyRegistry(service)
	if err != nil {
		t.Fatalf("newAuthPolicyRegistry: %v", err)
	}
	if _, ok := registry.lookup("/test.policy.TestService/Test"); !ok {
		t.Fatal("policy was not keyed by Transporter.Operation format")
	}
	if _, ok := registry.lookup("/some/http/path"); ok {
		t.Fatal("HTTP path unexpectedly resolved as a policy key")
	}
}

func policyTestFile(t *testing.T, methodName string, policy *policyv1.AuthPolicy) protoreflect.FileDescriptor {
	t.Helper()
	var options *descriptorpb.MethodOptions
	if policy != nil {
		options = &descriptorpb.MethodOptions{}
		proto.SetExtension(options, policyv1.E_AuthPolicy, policy)
	}
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("test/auth_policy.proto"),
		Package: proto.String("test.policy"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Request")},
			{Name: proto.String("Reply")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String(methodName),
				InputType:  proto.String(".test.policy.Request"),
				OutputType: proto.String(".test.policy.Reply"),
				Options:    options,
			}},
		}},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("construct test descriptor: %v", err)
	}
	return file
}
