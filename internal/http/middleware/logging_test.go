package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/ogrock3t/go-lw-avito/internal/http/middleware"
)

func TestLogging(t *testing.T) {
	var logs bytes.Buffer

	logger := slog.New(
		slog.NewJSONHandler(&logs, nil),
	)

	tracerProvider := sdktrace.NewTracerProvider()
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})

	ctx, span := tracerProvider.
		Tracer("test").
		Start(context.Background(), "test")
	defer span.End()

	router := chi.NewRouter()

	router.Use(
		middleware.Logging(logger),
	)

	router.Get(
		"/api/v1/trips/{tripId}",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/trips/2da91da4-40b1-43a9-b087-cbc599eb5934",
		nil,
	).WithContext(ctx)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	var record map[string]any

	if err := json.NewDecoder(&logs).Decode(&record); err != nil {
		t.Fatalf("decode log: %v", err)
	}

	if record["level"] != "ERROR" {
		t.Errorf(
			"level = %v, want ERROR",
			record["level"],
		)
	}

	if record["method"] != http.MethodGet {
		t.Errorf(
			"method = %v, want %s",
			record["method"],
			http.MethodGet,
		)
	}

	wantRoute := "/api/v1/trips/{tripId}"

	if record["route"] != wantRoute {
		t.Errorf(
			"route = %v, want %s",
			record["route"],
			wantRoute,
		)
	}

	if record["status"] != float64(
		http.StatusInternalServerError,
	) {
		t.Errorf(
			"status = %v, want %d",
			record["status"],
			http.StatusInternalServerError,
		)
	}

	if record["trace_id"] == "" {
		t.Error("trace_id is empty")
	}

	if record["span_id"] == "" {
		t.Error("span_id is empty")
	}
}
