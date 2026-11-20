package middleware_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/ogrock3t/go-lw-avito/internal/http/middleware"
)

func TestHTTPMetrics(t *testing.T) {
	const (
		route          = "/api/v1/trips/{tripId}"
		unmatchedRoute = "unmatched"
	)

	tests := []struct {
		name       string
		path       string
		status     int
		panic      bool
		writeBody  bool
		cancel     bool
		wantStatus int
		wantRoute  string
	}{
		{name: "explicit status", path: "/api/v1/trips/first", status: http.StatusCreated, wantStatus: http.StatusCreated, wantRoute: route},
		{name: "empty response", path: "/api/v1/trips/second", wantStatus: http.StatusOK, wantRoute: route},
		{name: "implicit status", path: "/api/v1/trips/third", writeBody: true, wantStatus: http.StatusOK, wantRoute: route},
		{name: "server error", path: "/api/v1/trips/fourth", status: http.StatusInternalServerError, wantStatus: http.StatusInternalServerError, wantRoute: route},
		{name: "unknown route", path: "/missing", wantStatus: http.StatusNotFound, wantRoute: unmatchedRoute},
		{name: "panic", path: "/api/v1/trips/fifth", panic: true, wantStatus: http.StatusInternalServerError, wantRoute: route},
		{name: "cancelled context", path: "/api/v1/trips/sixth", cancel: true, wantStatus: http.StatusOK, wantRoute: route},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() {
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Errorf("shutdown meter provider: %v", err)
				}
			})

			metrics, err := middleware.NewHTTPMetrics(provider)
			if err != nil {
				t.Fatalf("create HTTP metrics: %v", err)
			}

			router := chi.NewRouter()
			router.Use(metrics.Middleware(router))
			calls := 0
			router.Get(route, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				active := collectMetric(t, reader, "http.server.active_requests")
				sum, ok := active.Data.(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 {
					t.Fatalf("active requests data = %#v", active.Data)
				}
				if sum.DataPoints[0].Value != 1 {
					t.Errorf("active requests during handler = %d, want 1", sum.DataPoints[0].Value)
				}
				assertMetricAttributes(t, sum.DataPoints[0].Attributes, route, 0)

				if test.panic {
					panic("metrics test panic")
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				if test.writeBody {
					if _, err := w.Write([]byte("response")); err != nil {
						t.Errorf("write response: %v", err)
					}
				}
			})

			handler := middleware.Recover(slog.New(slog.NewJSONHandler(io.Discard, nil)))(router)
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.cancel {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Errorf("response status = %d, want %d", response.Code, test.wantStatus)
			}
			wantCalls := 1
			if test.wantRoute == unmatchedRoute {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Errorf("handler calls = %d, want %d", calls, wantCalls)
			}

			active := collectMetric(t, reader, "http.server.active_requests")
			sum, ok := active.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 {
				t.Fatalf("active requests data = %#v", active.Data)
			}
			if sum.DataPoints[0].Value != 0 {
				t.Errorf("active requests after handler = %d, want 0", sum.DataPoints[0].Value)
			}
			assertMetricAttributes(t, sum.DataPoints[0].Attributes, test.wantRoute, 0)

			duration := collectMetric(t, reader, "http.server.request.duration")
			if duration.Unit != "s" {
				t.Errorf("duration unit = %q, want s", duration.Unit)
			}
			histogram, ok := duration.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 {
				t.Fatalf("duration data = %#v", duration.Data)
			}
			point := histogram.DataPoints[0]
			if point.Count != 1 || point.Sum <= 0 {
				t.Errorf("duration count = %d, sum = %g; want one positive measurement", point.Count, point.Sum)
			}
			assertMetricAttributes(t, point.Attributes, test.wantRoute, test.wantStatus)
		})
	}
}

func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name == name {
				return measurement
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func assertMetricAttributes(t *testing.T, attributes attribute.Set, route string, status int) {
	t.Helper()
	want := attribute.NewSet(
		attribute.String("http.request.method", http.MethodGet),
		attribute.String("http.route", route),
	)
	if status != 0 {
		want = attribute.NewSet(
			attribute.String("http.request.method", http.MethodGet),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", status),
		)
	}
	if !attributes.Equals(&want) {
		t.Errorf("attributes = %v, want %v", attributes.ToSlice(), want.ToSlice())
	}
}
