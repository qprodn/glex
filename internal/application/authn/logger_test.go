package authn

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestApplicationsReceiveComponentLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	NewLoginApplication(nil, nil, nil, nil, logger).logger.Info("configured")
	NewOnlineSessionApplication(nil, nil, logger).logger.Info("configured")

	for _, component := range []string{
		"application.authn.login",
		"application.online_session",
	} {
		if !strings.Contains(output.String(), `"component":"`+component+`"`) {
			t.Fatalf("application log output %q does not contain component %q", output.String(), component)
		}
	}
}
