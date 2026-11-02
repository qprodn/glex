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

	NewLoginService(nil, nil, nil, nil, logger).(*loginImpl).logger.Info("configured")
	NewCredentialService(nil, logger).(*credentialImpl).logger.Info("configured")
	NewOnlineSessionService(nil, logger).(*onlineSessionImpl).logger.Info("configured")

	if !strings.Contains(output.String(), `"component":"domain.authn.svc"`) {
		t.Fatalf("domain log output %q does not contain component %q", output.String(), "domain.authn.svc")
	}
}
