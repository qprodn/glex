package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"glex/internal/conf"
	"glex/pkg/middleware/requestid"

	"github.com/google/uuid"
)

func TestHTTPServerReturnsServiceOwnedUUIDv7ForEveryResponse(t *testing.T) {
	server := NewHTTPServer(
		&conf.Server{Http: &conf.Server_HTTP{}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	)
	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		wantStatus  int
	}{
		{
			name:       "not found",
			method:     http.MethodGet,
			path:       "/not-found",
			wantStatus: http.StatusNotFound,
		},
		{
			name:        "binding failure",
			method:      http.MethodPost,
			path:        "/auth/login",
			body:        "{",
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body io.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			}
			req := httptest.NewRequest(test.method, test.path, body)
			req.Header.Set(requestid.Header, "caller-controlled")
			if test.contentType != "" {
				req.Header.Set("Content-Type", test.contentType)
			}
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			if res.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, test.wantStatus, res.Body.String())
			}
			got := res.Header().Get(requestid.Header)
			if got == "" || got == "caller-controlled" {
				t.Fatalf("response Request ID = %q, want service-generated value", got)
			}
			parsed, err := uuid.Parse(got)
			if err != nil {
				t.Fatalf("parse response Request ID %q: %v", got, err)
			}
			if parsed.Version() != 7 {
				t.Fatalf("response Request ID version = %d, want 7", parsed.Version())
			}
		})
	}
}
