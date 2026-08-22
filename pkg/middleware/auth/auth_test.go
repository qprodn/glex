package auth

import (
	"context"
	"errors"
	"testing"

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
	kind      transport.Kind
	header    transport.Header
}

func (t *testTransport) Kind() transport.Kind {
	if t.kind == "" {
		return transport.KindHTTP
	}
	return t.kind
}
func (t *testTransport) Endpoint() string                { return "http://test" }
func (t *testTransport) Operation() string               { return t.operation }
func (t *testTransport) RequestHeader() transport.Header { return t.header }
func (t *testTransport) ReplyHeader() transport.Header   { return testHeader{} }

type fakePrincipal struct {
	permissions []string
	roles       []string
}

func (p *fakePrincipal) HasPermission(permission string) bool {
	for _, granted := range p.permissions {
		if granted == permission || granted == "*:*:*" {
			return true
		}
	}
	return false
}

func (p *fakePrincipal) HasRole(roleKey string) bool {
	for _, granted := range p.roles {
		if granted == roleKey {
			return true
		}
	}
	return false
}

type fakeAuthenticator struct {
	principal Principal
	err       error
	calls     int
}

func (a *fakeAuthenticator) Authenticate(context.Context, string) (Principal, error) {
	a.calls++
	return a.principal, a.err
}

func bearerHeader() testHeader {
	return testHeader{"Authorization": {"Bearer test-credential"}}
}

const testOperation = "/test.Service/Test"

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
		{name: "empty header", header: "", err: ErrCredentialMalformed},
		{name: "bare value", header: "opaque-token", err: ErrCredentialMalformed},
		{name: "wrong scheme", header: "Basic opaque-token", err: ErrCredentialMalformed},
		{name: "empty token", header: "Bearer   ", err: ErrCredentialMalformed},
		{name: "tab separator", header: "Bearer\topaque-token", err: ErrCredentialMalformed},
		{name: "two values", header: "Bearer opaque-token other", err: ErrCredentialMalformed},
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

func TestMiddlewareEnforcesRouteChecks(t *testing.T) {
	valid := &fakePrincipal{roles: []string{"operator"}, permissions: []string{"system:user:list"}}
	superadmin := &fakePrincipal{permissions: []string{"*:*:*"}}
	tests := []struct {
		name          string
		route         Route
		header        testHeader
		principal     Principal
		authErr       error
		wantErr       error
		wantReason    string
		wantCode      int
		wantCalls     int
		wantPrincipal bool
	}{
		{name: "unlisted without token passes anonymously"},
		{name: "unlisted with malformed credential passes anonymously", header: testHeader{"Authorization": {"not-bearer"}}},
		{name: "unlisted with invalid token passes anonymously", header: bearerHeader(), authErr: ErrTokenInvalid, wantCalls: 1},
		{name: "unlisted with valid token publishes principal", header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "login missing credential", route: Route{Login: true}, wantErr: ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
		{name: "cookie does not authenticate", route: Route{Login: true}, header: testHeader{"Cookie": {"Authorization=Bearer cookie-token"}}, wantErr: ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
		{name: "login malformed credential", route: Route{Login: true}, header: testHeader{"Authorization": {"bare-token"}}, wantErr: ErrCredentialMalformed, wantReason: "CREDENTIAL_MALFORMED", wantCode: 401},
		{name: "invalid", route: Route{Login: true}, header: bearerHeader(), authErr: ErrTokenInvalid, wantErr: ErrTokenInvalid, wantReason: "TOKEN_INVALID", wantCode: 401, wantCalls: 1},
		{name: "expired", route: Route{Login: true}, header: bearerHeader(), authErr: ErrTokenExpired, wantErr: ErrTokenExpired, wantReason: "TOKEN_EXPIRED", wantCode: 401, wantCalls: 1},
		{name: "authenticator error propagates", route: Route{Login: true}, header: bearerHeader(), authErr: ErrAuthBackendUnavailable, wantErr: ErrAuthBackendUnavailable, wantReason: "AUTH_BACKEND_UNAVAILABLE", wantCode: 503, wantCalls: 1},
		{name: "nil principal is a backend fault", route: Route{Login: true}, header: bearerHeader(), wantErr: ErrAuthBackendUnavailable, wantReason: "AUTH_BACKEND_UNAVAILABLE", wantCode: 503, wantCalls: 1},
		{name: "login success", route: Route{Login: true}, header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "permission denied", route: Route{Permission: "system:user:edit"}, header: bearerHeader(), principal: valid, wantErr: ErrPermissionDenied, wantReason: "PERMISSION_DENIED", wantCode: 403, wantCalls: 1},
		{name: "permission exact", route: Route{Permission: "system:user:list"}, header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "permission superadmin wildcard", route: Route{Permission: "system:user:edit"}, header: bearerHeader(), principal: superadmin, wantCalls: 1, wantPrincipal: true},
		{name: "superadmin wildcard does not satisfy role check", route: Route{Role: "operator"}, header: bearerHeader(), principal: superadmin, wantErr: ErrRoleDenied, wantReason: "ROLE_DENIED", wantCode: 403, wantCalls: 1},
		{name: "role denied", route: Route{Role: "admin"}, header: bearerHeader(), principal: valid, wantErr: ErrRoleDenied, wantReason: "ROLE_DENIED", wantCode: 403, wantCalls: 1},
		{name: "role exact", route: Route{Role: "operator"}, header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "combined role and permission requires both", route: Route{Role: "operator", Permission: "system:user:edit"}, header: bearerHeader(), principal: valid, wantErr: ErrPermissionDenied, wantReason: "PERMISSION_DENIED", wantCode: 403, wantCalls: 1},
		{name: "combined role and permission satisfied", route: Route{Role: "operator", Permission: "system:user:list"}, header: bearerHeader(), principal: valid, wantCalls: 1, wantPrincipal: true},
		{name: "permission implies login", route: Route{Permission: "system:user:list"}, wantErr: ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
		{name: "role implies login", route: Route{Role: "operator"}, wantErr: ErrCredentialMissing, wantReason: "CREDENTIAL_MISSING", wantCode: 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := map[string]Route{}
			if tt.route != (Route{}) {
				routes[testOperation] = tt.route
			}
			authenticator := &fakeAuthenticator{principal: tt.principal, err: tt.authErr}
			ctx := transport.NewServerContext(t.Context(), &testTransport{operation: testOperation, header: tt.header})
			handlerCalled := false
			handler := Middleware(authenticator, routes)(func(ctx context.Context, _ any) (any, error) {
				handlerCalled = true
				_, hasPrincipal := PrincipalFromContext(ctx)
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

func TestMiddlewarePassesThroughNonHTTPTransport(t *testing.T) {
	authenticator := &fakeAuthenticator{principal: &fakePrincipal{}}
	next := middleware.Handler(func(context.Context, any) (any, error) { return "ok", nil })
	ctx := transport.NewServerContext(t.Context(), &testTransport{operation: "/test.Service/Test", kind: transport.KindGRPC, header: bearerHeader()})
	if _, err := Middleware(authenticator, nil)(next)(ctx, nil); err != nil {
		t.Fatalf("non-HTTP transport error = %v", err)
	}
}
