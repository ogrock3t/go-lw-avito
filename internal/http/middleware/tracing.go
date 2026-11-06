package middleware

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func Tracing(tracerProvider trace.TracerProvider) func(http.Handler) http.Handler {
	tracing := otelhttp.NewMiddleware(
		"http.server",
		otelhttp.WithTracerProvider(tracerProvider),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(
			func(_ string, r *http.Request) string {
				if r.Pattern == "" {
					return r.Method
				}

				return r.Method + " " + r.Pattern
			},
		),
		otelhttp.WithPropagators(
			propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			),
		),
	)

	return func(next http.Handler) http.Handler {
		return tracing(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			if state, ok := r.Context().Value(recoveryStateKey{}).(*recoveryState); ok {
				state.spanContext = span.SpanContext()
			}
			responseWriter := &statusResponseWriter{ResponseWriter: w}
			defer func() {
				if r.Pattern != "" {
					span.SetName(r.Method + " " + r.Pattern)
					span.SetAttributes(attribute.String("http.route", r.Pattern))
				}

				if recovered := recover(); recovered != nil {
					statusCode := responseWriter.StatusCode()
					if responseWriter.statusCode == 0 {
						statusCode = http.StatusInternalServerError
					}
					span.RecordError(fmt.Errorf("panic: %v", recovered))
					span.SetStatus(codes.Error, "panic recovered")
					span.SetAttributes(
						attribute.String("error.type", "panic"),
						attribute.Int("http.response.status_code", statusCode),
					)
					// Keep the outer Recover responsible for the HTTP response.
					panic(recovered)
				}
			}()

			next.ServeHTTP(responseWriter, r)
		}))
	}
}
