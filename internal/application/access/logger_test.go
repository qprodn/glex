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

func TestUsecasesReceiveComponentLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	NewMenuUsecase(nil, nil, nil, logger).logger.Info("configured")
	NewRoleUsecase(nil, nil, nil, nil, nil, logger).logger.Info("configured")
	NewRoleAssignmentUsecase(nil, nil, nil, nil, logger).logger.Info("configured")

	for _, component := range []string{
		"application.access.menu",
		"application.access.role",
		"application.access.role_assignment",
	} {
		if !strings.Contains(output.String(), `"component":"`+component+`"`) {
			t.Fatalf("usecase log output %q does not contain component %q", output.String(), component)
		}
	}
}
