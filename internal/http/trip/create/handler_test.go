package create_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/create"
	"github.com/ogrock3t/go-lw-avito/internal/model"
)

const problemContentType = "application/problem+json"

const validTripBody = `{
	"user_id": "550e8400-e29b-41d4-a716-446655440000",
	"driver_id": "550e8400-e29b-41d4-a716-446655440001",
	"start_point": {"latitude": 55.751244, "longitude": 37.618423},
	"end_point": {"latitude": 59.934280, "longitude": 30.335099},
	"price": 1500
}`

func TestHandler_CreateTrip_UseCaseResults(t *testing.T) {
	input := model.CreateTripInput{
		UserID:        uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
		DriverID:      uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"),
		StartLatitude: 55.751244, StartLongitude: 37.618423,
		EndLatitude: 59.934280, EndLongitude: 30.335099, Price: 1500,
	}
	created := trip.Trip{
		ID: uuid.New(), UserID: input.UserID, DriverID: input.DriverID,
		StartLatitude: input.StartLatitude, StartLongitude: input.StartLongitude,
		EndLatitude: input.EndLatitude, EndLongitude: input.EndLongitude, Price: input.Price,
		Status: trip.StatusActive, StartedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
	for _, busy := range []bool{false, true} {
		name := "success"
		if busy {
			name = "driver busy"
		}
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			useCase := NewMockuseCase(ctrl)
			var logs bytes.Buffer
			handler := create.NewHandler(useCase, time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))
			useCase.EXPECT().CreateTrip(gomock.Any(), input).DoAndReturn(
				func(ctx context.Context, _ model.CreateTripInput) (trip.Trip, error) {
					if _, ok := ctx.Deadline(); !ok {
						t.Error("usecase context has no timeout")
					}
					if busy {
						return trip.Trip{}, trip.ErrDriverBusy
					}
					return created, nil
				},
			)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validTripBody))
			response := httptest.NewRecorder()
			handler.CreateTrip(response, request, api.CreateTripParams{})
			if logs.Len() != 0 {
				t.Errorf("successful or client-error request logged an internal error: %s", logs.String())
			}
			if busy {
				var problem api.Problem
				if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if response.Code != http.StatusConflict || problem.Status != http.StatusConflict || problem.Code != "driver_busy" {
					t.Errorf("status = %d, problem = %+v", response.Code, problem)
				}
				if response.Header().Get("Content-Type") != problemContentType || problem.Instance == nil || *problem.Instance != request.URL.Path {
					t.Error("incorrect problem Content-Type or instance")
				}
				return
			}
			if response.Code != http.StatusCreated || response.Header().Get("Content-Type") != "application/json" {
				t.Errorf("status = %d, headers = %v", response.Code, response.Header())
			}
			if response.Header().Get("Location") != "/api/v1/trips/"+created.ID.String() {
				t.Errorf("Location = %q", response.Header().Get("Location"))
			}
			var got api.Trip
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode trip: %v", err)
			}
			want := api.Trip{
				Id: created.ID, UserId: input.UserID, DriverId: input.DriverID,
				StartPoint: api.Coordinates{Latitude: input.StartLatitude, Longitude: input.StartLongitude},
				EndPoint:   api.Coordinates{Latitude: input.EndLatitude, Longitude: input.EndLongitude},
				Price:      input.Price, Status: api.Active, StartedAt: created.StartedAt,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("trip = %+v, want %+v", got, want)
			}
		})
	}
}

func TestHandler_CreateTrip_InvalidFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	useCase := NewMockuseCase(ctrl)
	handler := create.NewHandler(useCase, time.Second, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader("{}"))
	response := httptest.NewRecorder()
	handler.CreateTrip(response, request, api.CreateTripParams{})
	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if response.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
		t.Errorf("status = %d, problem = %+v", response.Code, problem)
	}
}

func TestHandler_CreateTrip_InvalidBody(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	useCase := NewMockuseCase(ctrl)
	handler := create.NewHandler(useCase, time.Second, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/trips",
		strings.NewReader("{"),
	)
	response := httptest.NewRecorder()

	handler.CreateTrip(response, request, api.CreateTripParams{})

	result := response.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusBadRequest {
		t.Errorf("CreateTrip() status = %d, want %d", result.StatusCode, http.StatusBadRequest)
	}
	if contentType := result.Header.Get("Content-Type"); contentType != problemContentType {
		t.Errorf("CreateTrip() Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem api.Problem
	if err := json.NewDecoder(result.Body).Decode(&problem); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if problem.Status != http.StatusBadRequest {
		t.Errorf("CreateTrip() problem status = %d, want %d", problem.Status, http.StatusBadRequest)
	}
	if problem.Code != "invalid_request" {
		t.Errorf("CreateTrip() problem code = %q, want %q", problem.Code, "invalid_request")
	}
	if problem.Detail == nil || *problem.Detail != "Invalid JSON body" {
		t.Errorf("CreateTrip() problem detail = %v, want %q", problem.Detail, "Invalid JSON body")
	}
	if problem.Instance == nil || *problem.Instance != "/api/v1/trips" {
		t.Errorf("CreateTrip() problem instance = %v, want %q", problem.Instance, "/api/v1/trips")
	}
}

func TestHandler_CreateTrip_InternalErrorDoesNotLeak(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	useCase := NewMockuseCase(ctrl)
	var logs bytes.Buffer
	handler := create.NewHandler(useCase, time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))

	repositoryErr := errors.New("insert trip: database connection failed: password=secret")
	useCase.EXPECT().
		CreateTrip(gomock.Any(), gomock.Any()).
		Return(trip.Trip{}, repositoryErr)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/trips",
		strings.NewReader(`{
			"user_id": "550e8400-e29b-41d4-a716-446655440000",
			"driver_id": "550e8400-e29b-41d4-a716-446655440001",
			"start_point": {"latitude": 55.751244, "longitude": 37.618423},
			"end_point": {"latitude": 59.934280, "longitude": 30.335099},
			"price": 1500
		}`),
	)
	response := httptest.NewRecorder()

	handler.CreateTrip(response, request, api.CreateTripParams{})

	if response.Code != http.StatusInternalServerError {
		t.Errorf("CreateTrip() status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != problemContentType {
		t.Errorf("CreateTrip() Content-Type = %q, want %q", contentType, "application/problem+json")
	}
	if strings.Contains(response.Body.String(), repositoryErr.Error()) {
		t.Errorf("CreateTrip() response leaks internal error: %s", response.Body.String())
	}

	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if problem.Status != http.StatusInternalServerError {
		t.Errorf("CreateTrip() problem status = %d, want %d", problem.Status, http.StatusInternalServerError)
	}
	if problem.Code != "internal_error" {
		t.Errorf("CreateTrip() problem code = %q, want %q", problem.Code, "internal_error")
	}
	if problem.Detail == nil || *problem.Detail != "Internal server error" {
		t.Errorf("CreateTrip() problem detail = %v, want %q", problem.Detail, "Internal server error")
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatalf("decode error log: %v", err)
	}
	if record["error"] != repositoryErr.Error() || record["level"] != slog.LevelError.String() {
		t.Errorf("internal error log = %v", record)
	}
}
