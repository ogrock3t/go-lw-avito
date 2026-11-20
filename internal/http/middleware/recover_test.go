package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/middleware"
)

func TestRecover(t *testing.T) {
	var logs bytes.Buffer

	logger := slog.New(
		slog.NewJSONHandler(&logs, nil),
	)

	handler := middleware.Recover(logger)(
		http.HandlerFunc(func(
			http.ResponseWriter,
			*http.Request,
		) {
			panic("test panic")
		}),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/trips",
		nil,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Errorf(
			"status = %d, want %d",
			response.Code,
			http.StatusInternalServerError,
		)
	}

	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf(
			"Content-Type = %q, want %q",
			got,
			"application/problem+json",
		)
	}

	var problem api.Problem

	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if problem.Code != "internal_error" {
		t.Errorf(
			"problem code = %q, want %q",
			problem.Code,
			"internal_error",
		)
	}

	if !strings.Contains(logs.String(), "test panic") {
		t.Errorf(
			"log does not contain panic: %s",
			logs.String(),
		)
	}

	if !strings.Contains(logs.String(), "stack") {
		t.Errorf(
			"log does not contain stack: %s",
			logs.String(),
		)
	}
}
