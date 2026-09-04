package org

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

	NewDeptApplication(nil, nil, nil, logger).logger.Info("configured")
	NewPostApplication(nil, nil, nil, logger).logger.Info("configured")
	NewPostAssignmentApplication(nil, nil, logger).logger.Info("configured")

	for _, component := range []string{
		"application.org.dept",
		"application.org.post",
		"application.org.post_assignment",
	} {
		if !strings.Contains(output.String(), `"component":"`+component+`"`) {
			t.Fatalf("application log output %q does not contain component %q", output.String(), component)
		}
	}
}
