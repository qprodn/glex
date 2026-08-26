package v1

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v3/middleware/logging"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestUserWriteRequestsRedactPasswordWithoutMutation(t *testing.T) {
	tests := []struct {
		name     string
		request  interface{ Redact() string }
		password func() string
	}{
		{
			name: "create",
			request: &CreateUserRequest{User: &User{
				UserName: "operator",
				Password: "create-secret",
			}},
			password: func() string { return "create-secret" },
		},
		{
			name: "update",
			request: &UpdateUserRequest{
				User:       &User{UserId: 42, Password: "update-secret"},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"password"}},
			},
			password: func() string { return "update-secret" },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secret := test.password()
			got := test.request.Redact()
			if strings.Contains(got, secret) {
				t.Fatalf("redacted request leaked password: %s", got)
			}
			if !strings.Contains(got, `password:"***"`) {
				t.Fatalf("redacted request %q does not mark password as redacted", got)
			}
			switch request := test.request.(type) {
			case *CreateUserRequest:
				if request.User.Password != secret {
					t.Fatalf("Redact mutated create password to %q", request.User.Password)
				}
			case *UpdateUserRequest:
				if request.User.Password != secret {
					t.Fatalf("Redact mutated update password to %q", request.User.Password)
				}
			}
		})
	}
}

func TestKratosServerLoggingUsesUserWriteRequestRedaction(t *testing.T) {
	requests := []struct {
		name   string
		req    any
		secret string
	}{
		{
			name:   "create",
			req:    &CreateUserRequest{User: &User{UserName: "operator", Password: "create-secret"}},
			secret: "create-secret",
		},
		{
			name: "update",
			req: &UpdateUserRequest{
				User:       &User{UserId: 42, Password: "update-secret"},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"password"}},
			},
			secret: "update-secret",
		},
	}

	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			handler := logging.Server(logger)(func(context.Context, any) (any, error) {
				return nil, nil
			})

			if _, err := handler(context.Background(), test.req); err != nil {
				t.Fatalf("log request: %v", err)
			}
			got := output.String()
			if strings.Contains(got, test.secret) {
				t.Fatalf("Kratos server log leaked password: %s", got)
			}
			if !strings.Contains(got, `password:\"***\"`) {
				t.Fatalf("Kratos server log %q does not contain redacted password", got)
			}
		})
	}
}
