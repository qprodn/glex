package user

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestApplicationReceivesComponentLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	NewUserApplication(nil, nil, nil, nil, logger).logger.Info("configured")

	for _, component := range []string{
		"application.user",
	} {
		if !strings.Contains(output.String(), `"component":"`+component+`"`) {
			t.Fatalf("application log output %q does not contain component %q", output.String(), component)
		}
	}
}
