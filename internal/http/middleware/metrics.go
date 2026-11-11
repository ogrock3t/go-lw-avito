package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationName = "github.com/ogrock3t/go-lw-avito/internal/http/middleware"

type HTTPMetrics struct {
	requestDuration metric.Float64Histogram
	activeRequests  metric.Int64UpDownCounter
}

func NewHTTPMetrics(meterProvider metric.MeterProvider) (*HTTPMetrics, error) {
	meter := meterProvider.Meter(instrumentationName)

	requestDuration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP requests"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("create request duration histogram: %w", err)
	}

	activeRequests, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of active HTTP server requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("create active requests counter: %w", err)
	}

	return &HTTPMetrics{
		requestDuration: requestDuration,
		activeRequests:  activeRequests,
	}, nil
}

func (m *HTTPMetrics) Middleware(router chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			ctx := r.Context()
			route := matchedRoute(router, r)
			activeAttributes := metric.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("http.route", route),
			)

			m.activeRequests.Add(ctx, 1, activeAttributes)

			responseWriter := &statusResponseWriter{ResponseWriter: w}
			completed := false

			defer func() {
				m.activeRequests.Add(ctx, -1, activeAttributes)

				statusCode := responseWriter.StatusCode()

				if !completed && responseWriter.statusCode == 0 {
					statusCode = http.StatusInternalServerError
				}

				m.requestDuration.Record(
					ctx,
					time.Since(startedAt).Seconds(),
					metric.WithAttributes(
						attribute.String("http.request.method", r.Method),
						attribute.String("http.route", route),
						attribute.Int("http.response.status_code", statusCode),
					),
				)
			}()

			next.ServeHTTP(responseWriter, r)
			completed = true
		})
	}
}

func matchedRoute(router chi.Routes, r *http.Request) string {
	routeContext := chi.NewRouteContext()
	if !router.Match(routeContext, r.Method, r.URL.Path) {
		return "unmatched"
	}

	return routeContext.RoutePattern()
}
