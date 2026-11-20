package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/gateway/push"
)

func TestGateway_RequestPosition(t *testing.T) {
	driverID := uuid.New()
	tripID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/api/v1/push" {
				t.Errorf("path = %s, want /api/v1/push", r.URL.Path)
			}
			if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
				t.Errorf("Content-Type = %s, want application/json", contentType)
			}

			var body struct {
				RequestID   string            `json:"request_id"`
				RecipientID uuid.UUID         `json:"recipient_id"`
				Kind        string            `json:"kind"`
				Data        map[string]string `json:"data"`
			}

			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if _, err := uuid.Parse(body.RequestID); err != nil {
				t.Errorf("request_id = %q, want UUID", body.RequestID)
			}
			if body.RecipientID != driverID {
				t.Errorf("recipient_id = %s, want %s", body.RecipientID, driverID)
			}
			if body.Kind != "REQUEST_POSITION" {
				t.Errorf("kind = %s, want REQUEST_POSITION", body.Kind)
			}
			if body.Data["trip_id"] != tripID.String() {
				t.Errorf("trip_id = %s, want %s", body.Data["trip_id"], tripID)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(
				`{"message_id":"message-1","accepted_at":"2026-11-20T10:00:00Z"}`,
			))
		},
	))
	defer server.Close()

	gateway, err := push.NewGateway(server.URL, time.Second)
	if err != nil {
		t.Fatalf("create gateway: %v", err)
	}
	defer gateway.CloseIdleConnections()

	if err := gateway.RequestPosition(context.Background(), driverID, tripID); err != nil {
		t.Fatalf("RequestPosition() error: %v", err)
	}
}

func TestGateway_RequestPosition_HTTPError(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest,
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
		http.StatusOK,
		http.StatusTemporaryRedirect,
	}

	for _, status := range statuses {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Location", "/redirect-target")
					w.WriteHeader(status)
				},
			))
			defer server.Close()

			gateway, err := push.NewGateway(server.URL, time.Second)
			if err != nil {
				t.Fatalf("create gateway: %v", err)
			}
			defer gateway.CloseIdleConnections()

			err = gateway.RequestPosition(
				context.Background(),
				uuid.New(),
				uuid.New(),
			)

			var httpErr *push.HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("error = %v, want HTTPError", err)
			}
			if httpErr.StatusCode != status {
				t.Errorf("status = %d, want %d", httpErr.StatusCode, status)
			}
		})
	}
}

func TestGateway_RequestPosition_Timeout(t *testing.T) {
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, _ *http.Request) {
			<-release
		},
	))
	defer server.Close()
	defer close(release)

	gateway, err := push.NewGateway(server.URL, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("create gateway: %v", err)
	}
	defer gateway.CloseIdleConnections()

	err = gateway.RequestPosition(
		context.Background(),
		uuid.New(),
		uuid.New(),
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestGateway_RequestPosition_CanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		},
	))
	defer server.Close()

	gateway, err := push.NewGateway(server.URL, time.Second)
	if err != nil {
		t.Fatalf("create gateway: %v", err)
	}
	defer gateway.CloseIdleConnections()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = gateway.RequestPosition(ctx, uuid.New(), uuid.New())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
