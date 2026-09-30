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

	svc := NewConfigsService(nil, logger).(*configsImpl)
	svc.logger.Info("configured")

	if !strings.Contains(output.String(), `"component":"domain.param.svc"`) {
		t.Fatalf("domain log output %q does not contain component %q", output.String(), "domain.param.svc")
	}
}
