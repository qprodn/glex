package v1_test

import (
	"testing"

	authv1 "glex/api/system/auth/v1"
)

func TestAuthErrorReasonsAreStableAndComplete(t *testing.T) {
	want := []string{
		"ERROR_REASON_UNSPECIFIED",
		"INVALID_CREDENTIALS",
		"ACCOUNT_DISABLED",
		"LOGIN_LOCKED",
		"CREDENTIAL_MISSING",
		"CREDENTIAL_MALFORMED",
		"TOKEN_INVALID",
		"TOKEN_EXPIRED",
		"TOKEN_IDLE_TIMEOUT",
		"SESSION_KICKED_OUT",
		"SESSION_REPLACED",
		"PERMISSION_DENIED",
		"AUTH_BACKEND_UNAVAILABLE",
	}

	descriptor := authv1.ErrorReason_ERROR_REASON_UNSPECIFIED.Descriptor()
	if descriptor.Values().Len() != len(want) {
		t.Fatalf("auth error reason count = %d, want %d", descriptor.Values().Len(), len(want))
	}
	for i, name := range want {
		if got := string(descriptor.Values().Get(i).Name()); got != name {
			t.Errorf("auth error reason %d = %q, want %q", i, got, name)
		}
	}
}
