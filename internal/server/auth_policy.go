package server

import (
	"fmt"
	"sort"
	"strings"

	authv1 "glex/api/system/auth/v1"
	policyv1 "glex/api/system/auth/v1/policy"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type authPolicy struct {
	mode       policyv1.AuthMode
	permission string
}

// authPolicyRegistry is keyed by the exact value generated HTTP transports
// publish through Transporter.Operation().
type authPolicyRegistry struct {
	byOperation map[string]authPolicy
}

func newAuthPolicyRegistry(services ...protoreflect.ServiceDescriptor) (*authPolicyRegistry, error) {
	registry := &authPolicyRegistry{byOperation: make(map[string]authPolicy)}
	var methods []protoreflect.MethodDescriptor
	for _, service := range services {
		if service == nil {
			return nil, fmt.Errorf("auth policy validation: registered RPC descriptor is nil")
		}
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			methods = append(methods, service.Methods().Get(methodIndex))
		}
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].FullName() < methods[j].FullName() })
	if len(methods) == 0 {
		return nil, fmt.Errorf("auth policy validation: no RPC descriptors registered")
	}
	for _, method := range methods {
		operation := operationForMethod(method)
		policy, err := declaredAuthPolicy(method)
		if err != nil {
			return nil, fmt.Errorf("auth policy validation for %s: %w", operation, err)
		}
		if err := validateAuthPolicy(operation, policy); err != nil {
			return nil, fmt.Errorf("auth policy validation for %s: %w", operation, err)
		}
		if _, duplicate := registry.byOperation[operation]; duplicate {
			return nil, fmt.Errorf("auth policy validation: duplicate operation %s", operation)
		}
		registry.byOperation[operation] = authPolicy{mode: policy.GetMode(), permission: policy.GetPermission()}
	}
	return registry, nil
}

func operationForMethod(method protoreflect.MethodDescriptor) string {
	return "/" + string(method.Parent().FullName()) + "/" + string(method.Name())
}

func declaredAuthPolicy(method protoreflect.MethodDescriptor) (*policyv1.AuthPolicy, error) {
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

func validateAuthPolicy(operation string, policy *policyv1.AuthPolicy) error {
	permission := policy.GetPermission()
	switch policy.GetMode() {
	case policyv1.AuthMode_AUTH_MODE_UNSPECIFIED:
		return fmt.Errorf("auth policy mode is unspecified")
	case policyv1.AuthMode_PERMISSION:
		if strings.TrimSpace(permission) == "" {
			return fmt.Errorf("permission policy has no Functional Permission handle")
		}
		if permission != strings.TrimSpace(permission) {
			return fmt.Errorf("permission policy handle must not contain surrounding whitespace")
		}
	case policyv1.AuthMode_PUBLIC, policyv1.AuthMode_AUTHENTICATED:
		if permission != "" {
			return fmt.Errorf("%s policy must not carry a Functional Permission handle", policy.GetMode())
		}
	case policyv1.AuthMode_OPTIONAL:
		if permission != "" {
			return fmt.Errorf("OPTIONAL policy must not carry a Functional Permission handle")
		}
		if operation != authv1.OperationAuthServiceLogout {
			return fmt.Errorf("OPTIONAL policy is reserved for Logout")
		}
	default:
		return fmt.Errorf("auth policy mode %d is unknown", policy.GetMode())
	}
	return nil
}

func (r *authPolicyRegistry) lookup(operation string) (authPolicy, bool) {
	policy, ok := r.byOperation[operation]
	return policy, ok
}
