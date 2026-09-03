package logx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestPropagateLogsWarnWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	sentinel := errors.New("store down")
	Propagate(context.Background(), logger, "user.svc.create", sentinel)

	out := buf.String()
	for _, want := range []string{`"level":"WARN"`, `"operation":"user.svc.create"`, `"msg":"propagating error"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %s in log output, got: %s", want, out)
		}
	}
	if !strings.Contains(out, `"error":"store down"`) {
		t.Fatalf("expected the error carried in the error field, got: %s", out)
	}
}

func TestPropagateSkipsNilAndCanceled(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	Propagate(context.Background(), logger, "user.svc.create", nil)
	Propagate(context.Background(), logger, "user.svc.create", context.Canceled)
	// A wrapped cancellation is still benign.
	Propagate(context.Background(), logger, "user.svc.create",
		errors.Join(errors.New("outer"), context.Canceled))

	if buf.Len() != 0 {
		t.Fatalf("expected no log lines for nil/canceled errors, got: %s", buf.String())
	}
}
