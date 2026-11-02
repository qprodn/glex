// Package operlog is the operation-audit middleware (ADR 0012): after the
// handler returns it records one Oper Log row per mutating kratos
// operation — the operation name, operator, redacted request payload,
// outcome, and latency are all in scope exactly at this boundary. Recording
// is best-effort: a failed write logs Warn at this handling site (ADR 0007)
// and never fails the business response.
//
// The middleware declares its own Recorder port — pkg never imports
// internal/ (ADR 0010) — and the composition root adapts the audit domain
// service into it, the way the auth middleware receives its Authenticator.
package operlog

import (
	"context"
	"log/slog"
	"net"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	authmw "glex/pkg/middleware/auth"
	"glex/pkg/middleware/requestid"
)

// Entry is one recorded operation, the middleware's flat snapshot of the
// request. The recorder adapts it into the audit module's own types.
type Entry struct {
	Operation    string
	HTTPMethod   string
	OperUserID   int64
	OperUserName string
	IPAddr       string
	// Request is the protojson payload with secret keys scrubbed (ADR 0012).
	Request string
	// Success reports the recorded outcome.
	Success bool
	// Reason is the kratos error reason; empty on success.
	Reason    string
	ErrMsg    string
	LatencyMS int64
	RequestID string
}

// Recorder persists one operation record. Implementations must be safe for
// concurrent use and are expected to enforce their own timeout budget.
type Recorder interface {
	Record(ctx context.Context, entry *Entry) error
}

// Identity is the operator shape the middleware reads off the auth
// middleware's context value. The authn Principal satisfies it; asserting a
// narrow local interface keeps pkg decoupled from internal/.
type Identity interface {
	SystemUserID() int64
	UserName() string
}

// recordTimeout bounds one audit insert. The context is detached from the
// request (a disconnected client cannot cancel its own audit trail), so the
// budget must be explicit.
const recordTimeout = 3 * time.Second

// secretKeys are scrubbed from the request payload before anything is
// persisted; matching is by JSON key, at any depth.
var secretKeys = map[string]bool{
	"password":         true,
	"challenge_answer": true,
	"old_password":     true,
	"new_password":     true,
	"confirm_password": true,
	"access_token":     true,
	"refresh_token":    true,
}

// safeMethods never produce an Oper Log row: reads are not audited (ADR
// 0012 records what operators did, not what they looked at).
var safeMethods = map[string]bool{
	"GET":     true,
	"HEAD":    true,
	"OPTIONS": true,
}

type options struct {
	recorder Recorder
	skip     map[string]bool
	// scrubKeys are the JSON keys masked in recorded payloads: the static
	// set below plus whatever the composition root adds (the pay module's
	// schema-marked credential fields).
	scrubKeys map[string]bool
	logger    *slog.Logger
}

// Option configures the middleware.
type Option func(*options)

// WithSkip excludes operations from recording. authn's Login/Logout belong
// here — they are Login Log events (ADR 0012), not Oper Log ones.
func WithSkip(operations ...string) Option {
	return func(o *options) {
		for _, op := range operations {
			o.skip[op] = true
		}
	}
}

// WithSecretKeys adds module-specific secret field names to the scrub
// set (matched case-insensitively at any JSON depth, like the built-ins).
// The pay module feeds its channels' schema-marked secret fields here so
// merchant credential writes are never logged (ADR 0018).
func WithSecretKeys(keys ...string) Option {
	return func(o *options) {
		for _, key := range keys {
			o.scrubKeys[strings.ToLower(key)] = true
		}
	}
}

// WithLogger sets the logger used for recording failures; it defaults to
// slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// Server returns the operation-audit middleware.
func Server(recorder Recorder, opts ...Option) middleware.Middleware {
	o := options{
		recorder:  recorder,
		skip:      map[string]bool{},
		scrubKeys: make(map[string]bool, len(secretKeys)),
		logger:    slog.Default(),
	}
	for key := range secretKeys {
		o.scrubKeys[key] = true
	}
	for _, opt := range opts {
		opt(&o)
	}
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (reply any, err error) {
			start := time.Now()
			reply, err = handler(ctx, req)
			o.record(ctx, req, start, err)
			return
		}
	}
}

// record snapshots the finished request and persists it. Every path returns
// without touching the business outcome (ADR 0012).
func (o *options) record(ctx context.Context, req any, start time.Time, err error) {
	info, ok := transport.FromServerContext(ctx)
	if !ok || info.Operation() == "" {
		return
	}
	if o.skip[info.Operation()] {
		return
	}
	r := httpRequest(ctx)
	method := ""
	if r != nil {
		method = r.Method
	}
	if safeMethods[method] {
		return
	}
	entry := &Entry{
		Operation:  info.Operation(),
		HTTPMethod: method,
		RequestID:  requestID(ctx),
		IPAddr:     clientIP(ctx),
		Success:    err == nil,
		LatencyMS:  time.Since(start).Milliseconds(),
	}
	if principal := authmw.PrincipalFromContext(ctx); principal != nil {
		if identity, isBiz := principal.(Identity); isBiz {
			entry.OperUserID = identity.SystemUserID()
			entry.OperUserName = identity.UserName()
		}
	}
	if err != nil {
		if se := errors.FromError(err); se != nil {
			entry.Reason = se.Reason
			entry.ErrMsg = se.Message
		}
		if entry.ErrMsg == "" {
			entry.ErrMsg = err.Error()
		}
	}
	entry.Request = o.marshalRequest(req)
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if recordErr := o.recorder.Record(recordCtx, entry); recordErr != nil {
		o.logger.WarnContext(ctx, "oper log record failed",
			slog.String("operation", entry.Operation),
			slog.Any("error", recordErr),
		)
	}
}

// httpRequest returns the underlying *net/http request for an HTTP-kind
// exchange, nil otherwise (gRPC carries no *http.Request).
func httpRequest(ctx context.Context) *nethttp.Request {
	r, _ := khttp.RequestFromServerContext(ctx)
	return r
}

// requestID carries the correlation id minted by the requestid middleware.
func requestID(ctx context.Context) string {
	id, _ := requestid.RequestIDFromContext(ctx)
	return id
}

// clientIP reads the forwarded address chain's first hop, falling back to
// the socket's peer address; gRPC exchanges report the socket peer.
func clientIP(ctx context.Context) string {
	r := httpRequest(ctx)
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if idx := strings.IndexByte(forwarded, ','); idx > 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return forwarded
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// marshalRequest renders the request message as scrubbed protojson. A
// non-proto or unmarshalable request records a placeholder — auditing never
// blocks on the payload it is describing (ADR 0012).
func (o *options) marshalRequest(req any) string {
	msg, ok := req.(proto.Message)
	if !ok || msg == nil {
		return ""
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		o.logger.WarnContext(context.Background(), "oper log request marshal failed",
			slog.Any("error", err),
		)
		return "(unmarshalable request)"
	}
	return o.scrub(string(raw))
}

// scrub replaces the values of secret keys with a mask. Payloads are small
// JSON objects, so a scan-based rewrite is plenty.
func (o *options) scrub(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	rest := raw
	for {
		idx := strings.IndexByte(rest, '"')
		if idx < 0 {
			b.WriteString(rest)
			break
		}
		end := strings.IndexByte(rest[idx+1:], '"')
		if end < 0 {
			b.WriteString(rest)
			break
		}
		key := rest[idx+1 : idx+1+end]
		b.WriteString(rest[:idx])
		b.WriteByte('"')
		b.WriteString(key)
		b.WriteByte('"')
		rest = rest[idx+1+end+1:]
		if !o.scrubKeys[strings.ToLower(key)] {
			continue
		}
		// A secret key: keep valid JSON by splicing the mask over its value.
		colon := strings.IndexByte(rest, ':')
		if colon < 0 {
			continue
		}
		b.WriteString(rest[:colon+1])
		rest = rest[colon+1:]
		valueEnd := nextValueEnd(rest)
		b.WriteString(`"***"`)
		rest = rest[valueEnd:]
	}
	return b.String()
}

// nextValueEnd finds where the JSON value starting at rest[0] ends, from a
// position just past its colon: a quoted string (honoring escapes), or up
// to the first , } ] delimiter.
func nextValueEnd(rest string) int {
	if rest == "" {
		return 0
	}
	if rest[0] == '"' {
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
				continue
			}
			if rest[i] == '"' {
				return i + 1
			}
		}
		return len(rest)
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case ',', '}', ']':
			return i
		}
	}
	return len(rest)
}
