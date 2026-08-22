package iam

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

	NewUserUsecase(nil, nil, nil, logger).logger.Info("configured")
	NewMenuUsecase(nil, nil, nil, logger).logger.Info("configured")
	NewRoleUsecase(nil, nil, nil, nil, nil, logger).logger.Info("configured")
	NewAuthUsecase(nil, nil, nil, nil, nil, nil, nil, logger).logger.Info("configured")
	NewLoginSessionUsecase(nil, logger).logger.Info("configured")

	for _, component := range []string{
		"application.iam.user",
		"application.iam.menu",
		"application.iam.role",
		"application.iam.auth",
		"application.iam.login_session",
	} {
		if !strings.Contains(output.String(), `"component":"`+component+`"`) {
			t.Fatalf("usecase log output %q does not contain component %q", output.String(), component)
		}
	}
}
