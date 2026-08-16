package server

import (
	"context"
	"errors"
	"testing"

	policyv1 "glex/api/system/auth/v1/policy"
	"glex/internal/biz"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

type testHeader map[string][]string

func (h testHeader) Get(key string) string {
	values := h[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func (h testHeader) Set(key, value string) { h[key] = []string{value} }
func (h testHeader) Add(key, value string) { h[key] = append(h[key], value) }
func (h testHeader) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}
func (h testHeader) Values(key string) []string { return append([]string(nil), h[key]...) }

type testTransport struct {
	operation string
	header    transport.Header
}

func (t *testTransport) Kind() transport.Kind            { return transport.KindHTTP }
func (t *testTransport) Endpoint() string                { return "http://test" }
func (t *testTransport) Operation() string               { return t.operation }
func (t *testTransport) RequestHeader() transport.Header { return t.header }
func (t *testTransport) ReplyHeader() transport.Header   { return testHeader{} }

type fakeAuthenticator struct {
	principal *biz.Principal
	err       error
	calls     int
}

func (a *fakeAuthenticator) Authenticate(context.Context, string) (*biz.Principal, error) {
	a.calls++
	return a.principal, a.err
}

func TestStrictBearerCredentialParser(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
		err    error
	}{
		{name: "canonical", header: "Bearer opaque-token", want: "opaque-token"},
		{name: "case insensitive", header: "bEaReR opaque-token", want: "opaque-token"},
		{name: "multiple spaces", header: "Bearer   opaque-token", want: "opaque-token"},
		{name: "missing", err: biz.ErrCredentialMissing},
		{name: "bare value", header: "opaque-token", err: biz.ErrCredentialMalformed},
		{name: "wrong scheme", header: "Basic opaque-token", err: biz.ErrCredentialMalformed},
		{name: "empty token", header: "Bearer   ", err: biz.ErrCredentialMalformed},
		{name: "tab separator", header: "Bearer\topaque-token", err: biz.ErrCredentialMalformed},
		{name: "two values", header: "Bearer opaque-token other", err: biz.ErrCredentialMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBearerCredential(tt.header)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("parseBearerCredential result did not match expected presence/error: err=%v, wantErr=%v", err, tt.err)
			}
		})
	}
}

func TestAuthMiddlewarePolicyModesAndErrorMappings(t *testing.T) {
	valid := biz.NewPrincipal(42, "alice", "session-id", []string{"operator"}, []string{"system:user:list"})
	superadmin := biz.NewPrincipal(42, "alice", "session-id", []string{"superadmin"}, []string{"*:*:*"})
	tests := []struct {
		name          string
		policy        authPolicy
		header        testHeader
		principal     *biz.Principal
		authErr       error
		wantErr       error
		wantReason    string
		wantCode      int
		wantCalls     int
		wantPrincipal bool
	}{
		{name: "public does not inspect malformed credentials or storage", policy: authPolicy{mode: policyv1.AuthMode_PUBLIC}, header: testHeader{"Authorization": {"not-bearer"}}},
		{name: "authenticated missing", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, wantErr: biz.ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
		{name: "cookie does not authenticate", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: testHeader{"Cookie": {"Authorization=Bearer cookie-token"}}, wantErr: biz.ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
		{name: "query and body are unavailable to auth", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: testHeader{"Content-Type": {"application/json"}}, wantErr: biz.ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
		{name: "authenticated malformed", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: testHeader{"Authorization": {"bare-token"}}, wantErr: biz.ErrCredentialMalformed, wantReason: "CREDENTIAL_MALFORMED", wantCode: 401},
		{name: "invalid", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), authErr: biz.ErrTokenInvalid, wantErr: biz.ErrTokenInvalid, wantReason: "TOKEN_INVALID", wantCode: 401, wantCalls: 1},
		{name: "expired", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), authErr: biz.ErrTokenExpired, wantErr: biz.ErrTokenExpired, wantReason: "TOKEN_EXPIRED", wantCode: 401, wantCalls: 1},
		{name: "idle timeout", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), authErr: biz.ErrTokenIdleTimeout, wantErr: biz.ErrTokenIdleTimeout, wantReason: "TOKEN_IDLE_TIMEOUT", wantCode: 401, wantCalls: 1},
		{name: "kickout", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), authErr: biz.ErrLoginSessionKickedOut, wantErr: biz.ErrLoginSessionKickedOut, wantReason: "SESSION_KICKED_OUT", wantCode: 401, wantCalls: 1},
		{name: "replaced", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), authErr: biz.ErrLoginSessionReplaced, wantErr: biz.ErrLoginSessionReplaced, wantReason: "SESSION_REPLACED", wantCode: 401, wantCalls: 1},
		{name: "backend unavailable", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), authErr: biz.ErrAuthBackendUnavailable, wantErr: biz.ErrAuthBackendUnavailable, wantReason: "AUTH_BACKEND_UNAVAILABLE", wantCode: 503, wantCalls: 1},
		{name: "authenticated success", policy: authPolicy{mode: policyv1.AuthMode_AUTHENTICATED}, header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "permission denied", policy: authPolicy{mode: policyv1.AuthMode_PERMISSION, permission: "system:user:edit"}, header: bearerHeader(), principal: valid, wantErr: biz.ErrPermissionDenied, wantReason: "PERMISSION_DENIED", wantCode: 403, wantCalls: 1},
		{name: "permission exact", policy: authPolicy{mode: policyv1.AuthMode_PERMISSION, permission: "system:user:list"}, header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "permission superadmin wildcard", policy: authPolicy{mode: policyv1.AuthMode_PERMISSION, permission: "system:user:edit"}, header: bearerHeader(), principal: superadmin, wantCalls: 1, wantPrincipal: true},
		{name: "optional missing anonymous", policy: authPolicy{mode: policyv1.AuthMode_OPTIONAL}},
		{name: "optional invalid anonymous", policy: authPolicy{mode: policyv1.AuthMode_OPTIONAL}, header: bearerHeader(), authErr: biz.ErrTokenInvalid, wantCalls: 1},
		{name: "optional expired anonymous", policy: authPolicy{mode: policyv1.AuthMode_OPTIONAL}, header: bearerHeader(), authErr: biz.ErrTokenExpired, wantCalls: 1},
		{name: "optional malformed rejected", policy: authPolicy{mode: policyv1.AuthMode_OPTIONAL}, header: testHeader{"Authorization": {"bare-token"}}, wantErr: biz.ErrCredentialMalformed, wantReason: "CREDENTIAL_MALFORMED", wantCode: 401},
		{name: "optional idle remains 401", policy: authPolicy{mode: policyv1.AuthMode_OPTIONAL}, header: bearerHeader(), authErr: biz.ErrTokenIdleTimeout, wantErr: biz.ErrTokenIdleTimeout, wantReason: "TOKEN_IDLE_TIMEOUT", wantCode: 401, wantCalls: 1},
		{name: "optional backend remains 503", policy: authPolicy{mode: policyv1.AuthMode_OPTIONAL}, header: bearerHeader(), authErr: biz.ErrAuthBackendUnavailable, wantErr: biz.ErrAuthBackendUnavailable, wantReason: "AUTH_BACKEND_UNAVAILABLE", wantCode: 503, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const operation = "/test.Service/Test"
			policies := &authPolicyRegistry{byOperation: map[string]authPolicy{operation: tt.policy}}
			authenticator := &fakeAuthenticator{principal: tt.principal, err: tt.authErr}
			ctx := transport.NewServerContext(t.Context(), &testTransport{operation: operation, header: tt.header})
			handlerCalled := false
			handler := authMiddleware(authenticator, policies)(func(ctx context.Context, _ any) (any, error) {
				handlerCalled = true
				_, hasPrincipal := biz.PrincipalFromContext(ctx)
				if hasPrincipal != tt.wantPrincipal {
					t.Fatalf("handler Principal presence = %v, want %v", hasPrincipal, tt.wantPrincipal)
				}
				return "ok", nil
			})
			_, err := handler(ctx, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("middleware error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && !handlerCalled {
				t.Fatal("successful request did not reach handler")
			}
			if tt.wantErr != nil && handlerCalled {
				t.Fatal("rejected request reached handler")
			}
			if authenticator.calls != tt.wantCalls {
				t.Fatalf("Authenticate calls = %d, want %d", authenticator.calls, tt.wantCalls)
			}
			if tt.wantReason != "" {
				if got := kratoserrors.Reason(err); got != tt.wantReason {
					t.Fatalf("reason = %q, want %q", got, tt.wantReason)
				}
				if got := kratoserrors.Code(err); got != tt.wantCode {
					t.Fatalf("code = %d, want %d", got, tt.wantCode)
				}
			}
		})
	}
}

func TestAuthMiddlewareRejectsUnknownOperationAndNonHTTPTransport(t *testing.T) {
	authenticator := &fakeAuthenticator{}
	policies := &authPolicyRegistry{byOperation: map[string]authPolicy{}}
	next := middleware.Handler(func(context.Context, any) (any, error) { return nil, nil })
	ctx := transport.NewServerContext(t.Context(), &testTransport{operation: "/unknown", header: bearerHeader()})
	if _, err := authMiddleware(authenticator, policies)(next)(ctx, nil); !errors.Is(err, biz.ErrAuthBackendUnavailable) {
		t.Fatalf("unknown operation error = %v", err)
	}
}

func bearerHeader() testHeader {
	return testHeader{"Authorization": {"Bearer test-credential"}}
}
