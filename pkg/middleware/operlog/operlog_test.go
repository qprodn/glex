package operlog

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"

	dictv1 "glex/api/dict/v1"
	"glex/pkg/middleware/auth"
)

// The operlog middleware tests (ADR 0012): mutating operations are recorded
// with their outcome and a redacted payload; safe methods and skipped
// operations are not; a failing recorder never touches the business result.

const (
	createOp  = "/dict.v1.DictService/CreateDictType"
	listOp    = "/dict.v1.DictService/ListDictTypes"
	unknownOp = "/dict.v1.DictService/UpdateDictType"
)

// fakeHeader satisfies transport.Header over stdlib headers.
type fakeHeader http.Header

func (h fakeHeader) Get(key string) string      { return http.Header(h).Get(key) }
func (h fakeHeader) Set(key, value string)      { http.Header(h).Set(key, value) }
func (h fakeHeader) Add(key, value string)      { http.Header(h).Add(key, value) }
func (h fakeHeader) Keys() []string             { return nil }
func (h fakeHeader) Values(key string) []string { return http.Header(h).Values(key) }

// fakeTransporter is the server transport the middleware reads; it stands in
// for the kratos http Transport, whose fields are unexported.
type fakeTransporter struct {
	operation string
	request   *http.Request
}

func (f *fakeTransporter) Kind() transport.Kind            { return transport.KindHTTP }
func (f *fakeTransporter) Endpoint() string                { return "" }
func (f *fakeTransporter) Operation() string               { return f.operation }
func (f *fakeTransporter) RequestHeader() transport.Header { return fakeHeader{} }
func (f *fakeTransporter) ReplyHeader() transport.Header   { return fakeHeader{} }
func (f *fakeTransporter) Request() *http.Request          { return f.request }
func (f *fakeTransporter) PathTemplate() string            { return "" }

// testPrincipal satisfies both the auth middleware's and the middleware's
// Identity views of a principal.
type testPrincipal struct{}

func (testPrincipal) HasPermission(string) bool { return false }
func (testPrincipal) HasRole(string) bool       { return false }
func (testPrincipal) SystemUserID() int64       { return 42 }
func (testPrincipal) UserName() string          { return "alice" }

type fakeRecorder struct {
	entries []*Entry
	err     error
}

func (r *fakeRecorder) Record(_ context.Context, entry *Entry) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, entry)
	return nil
}

// invoke runs one request through the middleware-wrapped handler with a
// principal attached, like the auth middleware would have left it.
func invoke(t *testing.T, mw middleware.Middleware, op, method string, req any, handlerErr error) (any, error) {
	t.Helper()
	ctx := transport.NewServerContext(context.Background(), &fakeTransporter{
		operation: op,
		request:   (&http.Request{Method: method, RemoteAddr: "192.0.2.9:1234"}),
	})
	ctx = auth.WithPrincipal(ctx, testPrincipal{})
	return mw(func(context.Context, any) (any, error) {
		return "reply", handlerErr
	})(ctx, req)
}

func TestRecordsMutatingOperationWithRedactedPayload(t *testing.T) {
	recorder := &fakeRecorder{}
	mw := Server(recorder)

	req := &dictv1.CreateDictTypeRequest{DictType: &dictv1.DictType{DictName: "状态", DictType: "sys_state"}}
	// The secret key is spelled in the payload by position, not name — the
	// request message carries no password, so the scrub pass is pinned by
	// the dedicated scrub test below.
	out, err := invoke(t, mw, createOp, "POST", req, nil)
	if err != nil || out != "reply" {
		t.Fatalf("handler outcome changed: %v / %v", out, err)
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("entries = %+v, want one record", recorder.entries)
	}
	entry := recorder.entries[0]
	if entry.Operation != createOp || !entry.Success || entry.HTTPMethod != "POST" {
		t.Fatalf("entry = %+v, want the successful create", entry)
	}
	if entry.OperUserID != 42 || entry.OperUserName != "alice" {
		t.Fatalf("operator = %d/%q, want 42/alice from the principal", entry.OperUserID, entry.OperUserName)
	}
	if entry.IPAddr != "192.0.2.9" {
		t.Fatalf("ipaddr = %q, want the host part of RemoteAddr", entry.IPAddr)
	}
	if !strings.Contains(entry.Request, "sys_state") {
		t.Fatalf("request payload %q lost its content", entry.Request)
	}
}

func TestSkipsSafeMethodsAndSkippedOperations(t *testing.T) {
	recorder := &fakeRecorder{}
	mw := Server(recorder, WithSkip(createOp))

	if _, err := invoke(t, mw, listOp, "GET", &dictv1.ListDictTypesRequest{}, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := invoke(t, mw, createOp, "POST", &dictv1.CreateDictTypeRequest{}, nil); err != nil {
		t.Fatalf("skipped op: %v", err)
	}
	if len(recorder.entries) != 0 {
		t.Fatalf("entries = %+v, want none for reads and skipped ops", recorder.entries)
	}
}

func TestRecordsFailureOutcome(t *testing.T) {
	recorder := &fakeRecorder{}
	mw := Server(recorder)
	boom := errors.New("boom")

	_, err := invoke(t, mw, unknownOp, "DELETE", &dictv1.DeleteDictTypeRequest{DictTypeId: 1}, boom)
	if !errors.Is(err, boom) {
		t.Fatalf("handler error must pass through, got %v", err)
	}
	if len(recorder.entries) != 1 || recorder.entries[0].Success {
		t.Fatalf("entries = %+v, want one failure record", recorder.entries)
	}
	if recorder.entries[0].ErrMsg == "" {
		t.Fatal("failure record carries no error text")
	}
}

func TestSwallowsRecorderFailures(t *testing.T) {
	recorder := &fakeRecorder{err: errors.New("audit store down")}
	mw := Server(recorder)

	out, err := invoke(t, mw, createOp, "POST", &dictv1.CreateDictTypeRequest{}, nil)
	if err != nil || out != "reply" {
		t.Fatalf("recorder failure leaked into the response: %v / %v", out, err)
	}
}

func TestScrubMasksSecretKeys(t *testing.T) {
	o := newTestOptions()
	got := o.scrub(`{"user_name":"alice","password":"hunter2","profile":{"old_password":"a,b\"c"},"remark":"keep","tags":["x","y"]}`)
	want := `{"user_name":"alice","password":"***","profile":{"old_password":"***"},"remark":"keep","tags":["x","y"]}`
	if got != want {
		t.Fatalf("scrub = %s, want %s", got, want)
	}
}

func TestScrubMasksModuleSecretKeys(t *testing.T) {
	o := newTestOptions(WithSecretKeys("Merchant_Private_Key", "api_v3_key"))
	got := o.scrub(`{"config":{"merchant_private_key":"BEGIN","api_v3_key":"k","name":"主店"}}`)
	want := `{"config":{"merchant_private_key":"***","api_v3_key":"***","name":"主店"}}`
	if got != want {
		t.Fatalf("scrub = %s, want %s", got, want)
	}
}

// newTestOptions builds an options value the way Server does, without the
// middleware wrapper — scrub tests key on the scrub behavior alone.
func newTestOptions(opts ...Option) *options {
	o := options{
		recorder:  &fakeRecorder{},
		skip:      map[string]bool{},
		scrubKeys: make(map[string]bool, len(secretKeys)),
		logger:    slog.New(slog.DiscardHandler),
	}
	for key := range secretKeys {
		o.scrubKeys[key] = true
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &o
}
