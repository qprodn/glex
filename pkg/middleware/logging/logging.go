// Package logging is the transport request log, replacing kratos
// logging.Server (ADR 0011). Attributes and the "server request" message
// are identical to kratos's middleware; only the level differs, so ADR
// 0007's "business rejections never Error" holds at the boundary: kratos
// 4xx log Warn, a canceled request logs Info (benign, ADR 0007), and
// 5xx/unknown — the system failures — log Error with the stack.
package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/http/status"
	"google.golang.org/grpc/codes"
)

// Server is a drop-in replacement for kratos logging.Server with the
// ADR 0011 level demotion.
func Server(logger *slog.Logger) middleware.Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (reply any, err error) {
			var (
				code      int32
				reason    string
				kind      string
				operation string
			)

			// default code
			code = int32(status.FromGRPCCode(codes.OK))

			startTime := time.Now()
			if info, ok := transport.FromServerContext(ctx); ok {
				kind = info.Kind().String()
				operation = info.Operation()
			}
			reply, err = handler(ctx, req)
			if se := kratoserrors.FromError(err); se != nil {
				code = se.Code
				reason = se.Reason
			}
			level, stack := classify(err)
			attrs := []slog.Attr{
				slog.String("kind", "server"),
				slog.String("component", kind),
				slog.String("operation", operation),
				slog.String("args", extractArgs(req)),
				slog.Int64("code", int64(code)),
				slog.String("reason", reason),
				slog.Float64("latency", time.Since(startTime).Seconds()),
			}
			if err != nil {
				attrs = append(attrs, slog.Any("error", err))
				if stack != "" {
					attrs = append(attrs, slog.String("stack", stack))
				}
			}
			logger.LogAttrs(ctx, level, "server request", attrs...)
			return
		}
	}
}

// classify returns the log level for the outgoing error and the stack text
// to attach. Stack text is only worth carrying at Error — Warn/Info lines
// describe expected outcomes (ADR 0011).
func classify(err error) (slog.Level, string) {
	if err == nil {
		return slog.LevelInfo, ""
	}
	if errors.Is(err, context.Canceled) {
		return slog.LevelInfo, ""
	}
	stack := fmt.Sprintf("%+v", err)
	if se := kratoserrors.FromError(err); se != nil && se.Code < 500 {
		return slog.LevelWarn, ""
	}
	return slog.LevelError, stack
}

func extractArgs(req any) string {
	if stringer, ok := req.(fmt.Stringer); ok {
		return stringer.String()
	}
	return fmt.Sprintf("%+v", req)
}
