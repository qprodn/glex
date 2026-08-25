// Package requestid assigns one service-owned correlation identifier to each
// inbound request and publishes it through transport headers, context, and
// context-aware logs.
package requestid

import (
	"context"
	"log/slog"
	stdhttp "net/http"

	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/uuid"
)

const (
	// Header is the HTTP header and gRPC metadata key carrying a Request ID.
	Header = "X-Request-ID"
	// LogField is the structured logging field carrying a Request ID.
	LogField = "request_id"
)

var errGenerationFailed = errors.InternalServer(
	"REQUEST_ID_GENERATION_FAILED",
	"request ID generation failed",
)

// Generator creates a Request ID. It is injectable so generation failures can
// be tested without weakening production UUID generation.
type Generator func() (string, error)

type options struct {
	generate Generator
	logger   *slog.Logger
}

// Option configures the request ID HTTP filter or server middleware.
type Option func(*options)

// WithGenerator replaces the default UUIDv7 generator.
func WithGenerator(generate Generator) Option {
	return func(o *options) {
		if generate != nil {
			o.generate = generate
		}
	}
}

// WithLogger sets the logger used when Request ID generation fails.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

func newOptions(opts ...Option) options {
	o := options{
		generate: uuidV7,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func uuidV7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

type contextKey struct{}

func withRequestID(ctx context.Context, requestID string) context.Context {
	ctx = context.WithValue(ctx, contextKey{}, requestID)
	return log.ContextWithAttrs(ctx, slog.String(LogField, requestID))
}

// RequestIDFromContext returns the service-owned Request ID, when present.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(contextKey{}).(string)
	return requestID, ok && requestID != ""
}

// HTTP returns an outer Kratos HTTP filter. It runs before routing and request
// binding so even 404, 405, and malformed requests receive a Request ID.
func HTTP(opts ...Option) kratoshttp.FilterFunc {
	o := newOptions(opts...)
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
			requestID, err := o.generate()
			if err != nil {
				o.logger.ErrorContext(req.Context(), "request ID generation failed", slog.Any("error", err))
				kratoshttp.DefaultErrorEncoder(w, req, errGenerationFailed)
				return
			}

			req.Header.Set(Header, requestID)
			w.Header().Set(Header, requestID)
			ctx := withRequestID(req.Context(), requestID)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// Server returns a Kratos unary server middleware. It is used by gRPC; HTTP
// obtains its Request ID from the outer HTTP filter.
func Server(opts ...Option) middleware.Middleware {
	o := newOptions(opts...)
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			requestID, err := o.generate()
			if err != nil {
				o.logger.ErrorContext(ctx, "request ID generation failed", slog.Any("error", err))
				return nil, errGenerationFailed
			}

			if tr, ok := transport.FromServerContext(ctx); ok {
				if header := tr.RequestHeader(); header != nil {
					header.Set(Header, requestID)
				}
				if header := tr.ReplyHeader(); header != nil {
					header.Set(Header, requestID)
				}
			}
			return next(withRequestID(ctx, requestID), req)
		}
	}
}
