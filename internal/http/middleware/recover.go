package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"

	"go.opentelemetry.io/otel/trace"

	"github.com/ogrock3t/go-lw-avito/internal/http/response"
)

type recoveryStateKey struct{}

// Recover is outside Tracing, so share the inner span identity per request.
type recoveryState struct {
	spanContext trace.SpanContext
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	writer := response.NewWriter(logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &recoveryState{}
			r = r.WithContext(context.WithValue(r.Context(), recoveryStateKey{}, state))
			responseWriter := &statusResponseWriter{ResponseWriter: w}
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				attributes := []slog.Attr{
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
				}
				if state.spanContext.IsValid() {
					r = r.WithContext(trace.ContextWithSpanContext(r.Context(), state.spanContext))
					attributes = append(attributes,
						slog.String("trace_id", state.spanContext.TraceID().String()),
						slog.String("span_id", state.spanContext.SpanID().String()),
					)
				}
				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered", attributes...)

				// Once headers are sent, a new Problem response would corrupt the body.
				if responseWriter.statusCode != 0 {
					return
				}

				writer.WriteProblem(
					responseWriter,
					r,
					http.StatusInternalServerError,
					"internal_error",
					"Internal error",
					"Internal server error",
				)
			}()

			next.ServeHTTP(responseWriter, r)
		})
	}
}
