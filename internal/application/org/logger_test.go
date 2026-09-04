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

	NewDeptApplication(nil, nil, logger).logger.Info("configured")

	if !strings.Contains(output.String(), `"component":"application.org.dept"`) {
		t.Fatalf("application log output %q does not contain component %q", output.String(), "application.org.dept")
	}
}
