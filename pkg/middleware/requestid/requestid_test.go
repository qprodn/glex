package requestid

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/middleware/logging"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/google/uuid"
)

const testRequestID = "0198d3c2-4b1a-7f26-9d65-123456789abc"

func fixedGenerator() (string, error) { return testRequestID, nil }

func TestUUIDGeneratorProducesVersion7(t *testing.T) {
	requestID, err := uuidV7()
	if err != nil {
		t.Fatalf("generate UUIDv7: %v", err)
	}
	id, err := uuid.Parse(requestID)
	if err != nil {
		t.Fatalf("parse generated Request ID %q: %v", requestID, err)
	}
	if got := id.Version(); got != 7 {
		t.Fatalf("UUID version = %d, want 7", got)
	}
}

func TestHTTPOverwritesInboundIDAndPublishesGeneratedID(t *testing.T) {
	var contextID string
	handler := HTTP(WithGenerator(fixedGenerator))(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		contextID, _ = RequestIDFromContext(req.Context())
		if got := req.Header.Get(Header); got != testRequestID {
			t.Fatalf("request header = %q, want generated ID", got)
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	}))

	req := httptest.NewRequest(stdhttp.MethodGet, "/missing", nil)
	req.Header.Set(Header, "caller-controlled")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if contextID != testRequestID {
		t.Fatalf("context Request ID = %q, want %q", contextID, testRequestID)
	}
	if got := res.Header().Get(Header); got != testRequestID {
		t.Fatalf("response header = %q, want %q", got, testRequestID)
	}
}

func TestHTTPStopsWhenGenerationFails(t *testing.T) {
	var output bytes.Buffer
	logger := log.NewLogger(slog.NewJSONHandler(&output, nil))
	called := false
	handler := HTTP(
		WithGenerator(func() (string, error) { return "", errors.New("entropy unavailable") }),
		WithLogger(logger),
	)(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { called = true }))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(stdhttp.MethodGet, "/", nil))

	if called {
		t.Fatal("downstream handler ran after Request ID generation failure")
	}
	if res.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.Code)
	}
	if got := res.Header().Get(Header); got != "" {
		t.Fatalf("response Request ID = %q, want empty", got)
	}
	if !strings.Contains(output.String(), "entropy unavailable") {
		t.Fatalf("failure log %q does not contain generator error", output.String())
	}
}

func TestContextAttrsAddRequestIDToContextAwareLogs(t *testing.T) {
	var output bytes.Buffer
	logger := log.NewLogger(slog.NewJSONHandler(&output, nil))
	ctx := withRequestID(context.Background(), testRequestID)
	logger.InfoContext(ctx, "handling request")

	if !strings.Contains(output.String(), `"request_id":"`+testRequestID+`"`) {
		t.Fatalf("log output %q does not contain Request ID", output.String())
	}
}

func TestServerOverwritesTransportHeadersAndPublishesContext(t *testing.T) {
	tr := &testTransport{
		request: testHeader{strings.ToLower(Header): "caller-controlled"},
		reply:   testHeader{},
	}
	ctx := transport.NewServerContext(context.Background(), tr)

	called := false
	handler := Server(WithGenerator(fixedGenerator))(func(ctx context.Context, _ any) (any, error) {
		called = true
		if got, ok := RequestIDFromContext(ctx); !ok || got != testRequestID {
			t.Fatalf("context Request ID = %q/%v, want %q/true", got, ok, testRequestID)
		}
		return "reply", nil
	})
	reply, err := handler(ctx, "request")
	if err != nil || reply != "reply" || !called {
		t.Fatalf("handler result = %v, %v, called=%v", reply, err, called)
	}
	if got := tr.request.Get(Header); got != testRequestID {
		t.Fatalf("request metadata = %q, want %q", got, testRequestID)
	}
	if got := tr.reply.Get(Header); got != testRequestID {
		t.Fatalf("reply metadata = %q, want %q", got, testRequestID)
	}
}

func TestServerMakesRequestIDAvailableToKratosLoggingServer(t *testing.T) {
	var output bytes.Buffer
	logger := log.NewLogger(slog.NewJSONHandler(&output, nil))
	tr := &testTransport{request: testHeader{}, reply: testHeader{}}
	ctx := transport.NewServerContext(context.Background(), tr)
	handler := Server(WithGenerator(fixedGenerator))(
		logging.Server(logger)(func(context.Context, any) (any, error) {
			return "reply", nil
		}),
	)

	if _, err := handler(ctx, "request"); err != nil {
		t.Fatalf("handle request: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, `"msg":"server request"`) ||
		!strings.Contains(got, `"request_id":"`+testRequestID+`"`) {
		t.Fatalf("Kratos server log %q does not contain the Request ID", got)
	}
}

func TestServerStopsWhenGenerationFails(t *testing.T) {
	var output bytes.Buffer
	logger := log.NewLogger(slog.NewJSONHandler(&output, nil))
	called := false
	handler := Server(
		WithGenerator(func() (string, error) { return "", errors.New("entropy unavailable") }),
		WithLogger(logger),
	)(func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})

	reply, err := handler(context.Background(), "request")
	if called || reply != nil {
		t.Fatalf("downstream handler result = %v, called=%v; want nil/false", reply, called)
	}
	if got := kratoserrors.FromError(err); got.Code != 500 || got.Reason != "REQUEST_ID_GENERATION_FAILED" {
		t.Fatalf("error = %#v, want 500 REQUEST_ID_GENERATION_FAILED", got)
	}
	if !strings.Contains(output.String(), "entropy unavailable") {
		t.Fatalf("failure log %q does not contain generator error", output.String())
	}
}

type testHeader map[string]string

func (h testHeader) Get(key string) string      { return h[strings.ToLower(key)] }
func (h testHeader) Set(key, value string)      { h[strings.ToLower(key)] = value }
func (h testHeader) Add(key, value string)      { h.Set(key, value) }
func (h testHeader) Keys() []string             { return nil }
func (h testHeader) Values(key string) []string { return []string{h.Get(key)} }

type testTransport struct {
	request testHeader
	reply   testHeader
}

func (*testTransport) Kind() transport.Kind               { return transport.KindGRPC }
func (*testTransport) Endpoint() string                   { return "grpc://test" }
func (*testTransport) Operation() string                  { return "/test.Service/Call" }
func (tr *testTransport) RequestHeader() transport.Header { return tr.request }
func (tr *testTransport) ReplyHeader() transport.Header   { return tr.reply }
