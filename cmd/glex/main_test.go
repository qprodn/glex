package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLoggerAddsServiceAttributesAndRedactsSensitiveValues(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(slog.NewJSONHandler(&output, nil))

	logger.Info("logger configured",
		slog.String("password", "plain-text-secret"),
		slog.String("safe", "visible"),
	)

	got := output.String()
	for _, want := range []string{
		`"service.id"`,
		`"service.name":"` + Name + `"`,
		`"service.version"`,
		`"password":"***"`,
		`"safe":"visible"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "plain-text-secret") {
		t.Fatalf("sensitive value leaked into log output: %s", got)
	}
}
