package payinfra

import "log/slog"

// testLogger discards records — limiter tests assert behavior, not logs.
func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
