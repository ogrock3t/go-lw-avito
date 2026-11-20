package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type statusResponseWriter struct {
	http.ResponseWriter

	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}

	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Write(body []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}

	return w.ResponseWriter.Write(body)
}

func (w *statusResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusResponseWriter) StatusCode() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}

	return w.statusCode
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()

			responseWriter := &statusResponseWriter{ResponseWriter: w}

			completed := false
			defer func() {
				statusCode := responseWriter.StatusCode()
				if !completed && responseWriter.statusCode == 0 {
					statusCode = http.StatusInternalServerError
				}

				route := r.Pattern
				if route == "" {
					route = "unmatched"
				}

				attributes := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("route", route),
					slog.Int("status", statusCode),
					slog.Int64(
						"duration_ms",
						time.Since(startedAt).Milliseconds(),
					),
				}

				spanContext := trace.SpanContextFromContext(r.Context())

				if spanContext.IsValid() {
					attributes = append(
						attributes,
						slog.String("trace_id", spanContext.TraceID().String()),
						slog.String("span_id", spanContext.SpanID().String()),
					)
				}

				level := slog.LevelInfo
				if !completed || statusCode >= http.StatusInternalServerError {
					level = slog.LevelError
				}

				logger.LogAttrs(
					r.Context(),
					level,
					"request handled",
					attributes...,
				)
			}()

			next.ServeHTTP(responseWriter, r)
			completed = true
		})
	}
}
