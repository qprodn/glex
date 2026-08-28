package access

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

	NewMenuApplication(nil, nil, nil, logger).logger.Info("configured")
	NewRoleApplication(nil, nil, nil, logger).logger.Info("configured")
	NewRoleAssignmentApplication(nil, nil, nil, logger).logger.Info("configured")

	for _, component := range []string{
		"application.access.menu",
		"application.access.role",
		"application.access.role_assignment",
	} {
		if !strings.Contains(output.String(), `"component":"`+component+`"`) {
			t.Fatalf("application log output %q does not contain component %q", output.String(), component)
		}
	}
}
