package v1

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v3/middleware/logging"
)

func TestLoginRequestRedactHidesPasswordWithoutMutatingRequest(t *testing.T) {
	req := &LoginRequest{Username: "operator", Password: "plain-text-secret", ChallengeAnswer: "AB23"}
	got := req.Redact()

	if strings.Contains(got, req.Password) {
		t.Fatalf("redacted request leaked password: %s", got)
	}
	if strings.Contains(got, req.ChallengeAnswer) {
		t.Fatalf("redacted request leaked challenge answer: %s", got)
	}
	if !strings.Contains(got, `password:"***"`) {
		t.Fatalf("redacted request %q does not mark password as redacted", got)
	}
	if !strings.Contains(got, `challenge_answer:"***"`) {
		t.Fatalf("redacted request %q does not mark challenge answer as redacted", got)
	}
	if req.Password != "plain-text-secret" || req.ChallengeAnswer != "AB23" {
		t.Fatalf("Redact mutated request to password %q answer %q", req.Password, req.ChallengeAnswer)
	}
}

func TestKratosServerLoggingUsesLoginRequestRedaction(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	req := &LoginRequest{Username: "operator", Password: "plain-text-secret", ChallengeAnswer: "AB23"}
	handler := logging.Server(logger)(func(context.Context, any) (any, error) {
		return nil, nil
	})

	if _, err := handler(context.Background(), req); err != nil {
		t.Fatalf("log request: %v", err)
	}
	got := output.String()
	if strings.Contains(got, req.Password) || strings.Contains(got, req.ChallengeAnswer) {
		t.Fatalf("Kratos server log leaked a secret: %s", got)
	}
	if !strings.Contains(got, `password:\"***\"`) {
		t.Fatalf("Kratos server log %q does not contain redacted password", got)
	}
}
