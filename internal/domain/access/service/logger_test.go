package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestServiceReceivesComponentLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	NewRoleService(nil, nil, nil, logger).(*roleImpl).logger.Info("configured")
	NewMenuService(nil, logger).(*menuImpl).logger.Info("configured")
	NewRoleAssignmentService(nil, nil, logger).(*roleAssignmentImpl).logger.Info("configured")
	NewAuthorizationService(nil, logger).(*authorizationImpl).logger.Info("configured")

	if !strings.Contains(output.String(), `"component":"domain.access.svc"`) {
		t.Fatalf("domain log output %q does not contain component %q", output.String(), "domain.access.svc")
	}
}
