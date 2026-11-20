package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ogrock3t/go-lw-avito/internal/http/middleware"
)

func TestTracing(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})

	router := chi.NewRouter()

	router.Use(
		middleware.Tracing(tracerProvider),
	)

	router.Get(
		"/api/v1/trips/{tripId}",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/trips/2da91da4-40b1-43a9-b087-cbc599eb5934",
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf(
			"ended spans = %d, want 1",
			len(spans),
		)
	}

	wantName := "GET /api/v1/trips/{tripId}"

	if spans[0].Name() != wantName {
		t.Errorf(
			"span name = %q, want %q",
			spans[0].Name(),
			wantName,
		)
	}

	var route string

	for _, attr := range spans[0].Attributes() {
		if string(attr.Key) == "http.route" {
			route = attr.Value.AsString()
		}
	}

	if route != "/api/v1/trips/{tripId}" {
		t.Errorf(
			"http.route = %q, want %q",
			route,
			"/api/v1/trips/{tripId}",
		)
	}
}
