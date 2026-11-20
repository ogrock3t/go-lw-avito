package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ogrock3t/go-lw-avito/internal/http/middleware"
)

func TestTracingAndLogging(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		logLevel string
		spanCode codes.Code
	}{
		{
			name:     "success",
			status:   http.StatusOK,
			logLevel: "INFO",
			spanCode: codes.Unset,
		},
		{
			name:     "not found",
			status:   http.StatusNotFound,
			logLevel: "INFO",
			spanCode: codes.Unset,
		},
		{
			name:     "internal error",
			status:   http.StatusInternalServerError,
			logLevel: "ERROR",
			spanCode: codes.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(recorder),
			)
			t.Cleanup(func() {
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Errorf("shutdown provider: %v", err)
				}
			})

			router := chi.NewRouter()
			router.Use(
				middleware.Tracing(provider),
				middleware.Logging(logger),
			)

			const route = "/api/v1/trips/{tripId}"

			router.Get(route, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
			})

			const incomingTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
			const incomingSpanID = "00f067aa0ba902b7"

			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/trips/2da91da4-40b1-43a9-b087-cbc599eb5934",
				nil,
			)
			request.Header.Set(
				"traceparent",
				"00-"+incomingTraceID+"-"+incomingSpanID+"-01",
			)

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("spans = %d, want 1", len(spans))
			}
			span := spans[0]

			if span.Name() != "GET "+route {
				t.Errorf("span name = %q, want %q", span.Name(), "GET "+route)
			}

			if span.Status().Code != test.spanCode {
				t.Errorf("span status = %v, want %v", span.Status().Code, test.spanCode)
			}

			if span.SpanContext().TraceID().String() != incomingTraceID {
				t.Error("request did not continue the incoming trace")
			}
			if span.Parent().SpanID().String() != incomingSpanID {
				t.Error("incoming span is not the parent")
			}
			if !span.Parent().IsRemote() {
				t.Error("parent must be remote")
			}

			var gotRoute string
			for _, attr := range span.Attributes() {
				if string(attr.Key) == "http.route" {
					gotRoute = attr.Value.AsString()
				}
			}
			if gotRoute != route {
				t.Errorf("http.route = %q, want %q", gotRoute, route)
			}

			decoder := json.NewDecoder(&logs)
			var record map[string]any
			if err := decoder.Decode(&record); err != nil {
				t.Fatalf("decode log: %v", err)
			}

			wantFields := map[string]any{
				"msg":      "request handled",
				"method":   http.MethodGet,
				"route":    route,
				"status":   float64(test.status),
				"level":    test.logLevel,
				"trace_id": span.SpanContext().TraceID().String(),
				"span_id":  span.SpanContext().SpanID().String(),
			}

			for key, want := range wantFields {
				if got := record[key]; got != want {
					t.Errorf("log[%q] = %v, want %v", key, got, want)
				}
			}

			duration, ok := record["duration_ms"].(float64)
			if !ok || duration < 0 {
				t.Errorf("invalid duration_ms: %v", record["duration_ms"])
			}

			var extraRecord map[string]any
			if err := decoder.Decode(&extraRecord); err != io.EOF {
				t.Errorf("expected exactly one log record, got error: %v", err)
			}
		})
	}
}
