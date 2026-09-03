package logging

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
)

func TestClassifyLevels(t *testing.T) {
	notFound := kratoserrors.NotFound("USER_NOT_FOUND", "user not found")
	unavailable := kratoserrors.ServiceUnavailable("USER_STORE_UNAVAILABLE", "user store unavailable")
	plain := errors.New("driver down")

	for _, tc := range []struct {
		name string
		err  error
		want slog.Level
	}{
		{"nil is the request record", nil, slog.LevelInfo},
		{"canceled is benign", context.Canceled, slog.LevelInfo},
		{"wrapped cancellation is benign", errors.Join(plain, context.Canceled), slog.LevelInfo},
		{"4xx business rejection warns", notFound, slog.LevelWarn},
		{"wrapped 4xx business rejection warns", errors.Join(notFound), slog.LevelWarn},
		{"5xx sentinel errors", unavailable, slog.LevelError},
		{"unknown non-kratos error errors", plain, slog.LevelError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			level, _ := classify(tc.err)
			if level != tc.want {
				t.Fatalf("classify(%v) = %v, want %v", tc.err, level, tc.want)
			}
		})
	}
}

func TestClassifyStackOnlyAtError(t *testing.T) {
	if _, stack := classify(kratoserrors.NotFound("USER_NOT_FOUND", "user not found")); stack != "" {
		t.Fatalf("expected no stack on a Warn business rejection, got %q", stack)
	}
	if _, stack := classify(errors.New("driver down")); stack == "" {
		t.Fatal("expected a stack on an Error-level unknown failure")
	}
}
