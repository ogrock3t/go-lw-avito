package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/middleware"
)

func TestMiddlewareChain_Panic(t *testing.T) {
	const route = "/api/v1/trips/{tripId}"
	const panicDetail = "private database failure"

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracing: %v", err)
		}
		if err := meterProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown metrics: %v", err)
		}
	})
	metrics, err := middleware.NewHTTPMetrics(meterProvider)
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}
	router := chi.NewRouter()
	router.Use(
		middleware.Tracing(tracerProvider),
		middleware.Logging(logger),
		metrics.Middleware(router),
	)
	router.Get(route, func(http.ResponseWriter, *http.Request) {
		panic(panicDetail)
	})
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.Recover(logger)(router)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/trips/123", nil))

	if response.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q", got)
	}
	if strings.Contains(response.Body.String(), panicDetail) {
		t.Error("response leaks panic details")
	}
	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "internal_error" || problem.Status != http.StatusInternalServerError {
		t.Errorf("problem = %+v", problem)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name() != "GET "+route || span.Status().Code != codes.Error {
		t.Errorf("span name = %q, status = %v", span.Name(), span.Status())
	}
	attributes := attribute.NewSet(span.Attributes()...)
	assertSpanAttribute(t, attributes, "http.route", attribute.StringValue(route))
	assertSpanAttribute(t, attributes, "http.response.status_code", attribute.IntValue(http.StatusInternalServerError))
	assertSpanAttribute(t, attributes, "error.type", attribute.StringValue("panic"))
	if len(span.Events()) == 0 {
		t.Error("panic span has no exception event")
	}
	assertPanicLogs(t, &logs, span.SpanContext().TraceID().String(), span.SpanContext().SpanID().String(), route)
	assertPanicMetrics(t, reader, route)

	healthResponse := httptest.NewRecorder()
	handler.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthResponse.Code != http.StatusOK {
		t.Errorf("request after panic returned %d, want 200", healthResponse.Code)
	}
}

func assertSpanAttribute(t *testing.T, attributes attribute.Set, key string, want attribute.Value) {
	t.Helper()
	got, ok := attributes.Value(attribute.Key(key))
	if !ok || got != want {
		t.Errorf("span attribute %s = %v, want %v", key, got, want)
	}
}

func assertPanicLogs(t *testing.T, logs io.Reader, traceID, spanID, route string) {
	t.Helper()
	decoder := json.NewDecoder(logs)
	counts := make(map[string]int)
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		message, _ := record["msg"].(string)
		counts[message]++
		if record["trace_id"] != traceID || record["span_id"] != spanID {
			t.Errorf("log %q has incorrect trace correlation: %v", message, record)
		}
		if record["level"] != slog.LevelError.String() {
			t.Errorf("log %q level = %v, want ERROR", message, record["level"])
		}
		switch message {
		case "request handled":
			if record["status"] != float64(http.StatusInternalServerError) || record["route"] != route {
				t.Errorf("access log = %v", record)
			}
		case "panic recovered":
			if stack, _ := record["stack"].(string); stack == "" {
				t.Error("panic log has no stack trace")
			}
		}
	}
	if counts["request handled"] != 1 || counts["panic recovered"] != 1 {
		t.Errorf("log counts = %v, want one access log and one panic log", counts)
	}
}

func TestRecover_ResponseAlreadyStarted(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := middleware.Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte("started")); err != nil {
			t.Errorf("write response: %v", err)
		}
		panic("panic after response started")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusAccepted || response.Body.String() != "started" {
		t.Errorf("response = %d %q, want original response without appended Problem", response.Code, response.Body.String())
	}
}

func assertPanicMetrics(t *testing.T, reader *sdkmetric.ManualReader, route string) {
	t.Helper()
	active := collectMetric(t, reader, "http.server.active_requests")
	sum, ok := active.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("active requests data = %#v", active.Data)
	}
	if sum.DataPoints[0].Value != 0 {
		t.Errorf("active requests = %d, want 0", sum.DataPoints[0].Value)
	}
	assertMetricAttributes(t, sum.DataPoints[0].Attributes, route, 0)
	duration := collectMetric(t, reader, "http.server.request.duration")
	histogram, ok := duration.Data.(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 {
		t.Fatalf("duration data = %#v", duration.Data)
	}
	if histogram.DataPoints[0].Count != 1 {
		t.Errorf("duration count = %d, want 1", histogram.DataPoints[0].Count)
	}
	assertMetricAttributes(t, histogram.DataPoints[0].Attributes, route, http.StatusInternalServerError)
}
