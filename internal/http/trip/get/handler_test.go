package get_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/get"
)

func TestHandler_GetTrip(t *testing.T) {
	internalErr := errors.New("private database connection failure")
	fixture := trip.Trip{
		ID: uuid.New(), UserID: uuid.New(), DriverID: uuid.New(), Price: 1500,
		StartLatitude: 55.7, StartLongitude: 37.6, EndLatitude: 59.9, EndLongitude: 30.3,
		Status: trip.StatusActive, StartedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "success", status: http.StatusOK},
		{name: "missing", err: trip.ErrTripNotFound, status: http.StatusNotFound, code: "trip_not_found"},
		{name: "internal error", err: internalErr, status: http.StatusInternalServerError, code: "internal_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			useCase := NewMockuseCase(ctrl)
			var logs bytes.Buffer
			handler := get.NewHandler(useCase, time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))
			useCase.EXPECT().GetTrip(gomock.Any(), fixture.ID).DoAndReturn(
				func(ctx context.Context, _ uuid.UUID) (trip.Trip, error) {
					if _, ok := ctx.Deadline(); !ok {
						t.Error("usecase context has no timeout")
					}
					return fixture, test.err
				},
			)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+fixture.ID.String(), nil)
			response := httptest.NewRecorder()
			handler.GetTrip(response, request, fixture.ID)
			if response.Code != test.status {
				t.Errorf("status = %d, want %d", response.Code, test.status)
			}
			if test.err != nil {
				var problem api.Problem
				if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if problem.Code != test.code || int(problem.Status) != test.status || problem.Instance == nil || *problem.Instance != request.URL.Path {
					t.Errorf("problem = %+v", problem)
				}
				if response.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(response.Body.String(), internalErr.Error()) {
					t.Error("invalid problem Content-Type or leaked internal error")
				}
				if test.status == http.StatusInternalServerError {
					if !strings.Contains(logs.String(), internalErr.Error()) {
						t.Error("internal error was not logged")
					}
				} else if logs.Len() != 0 {
					t.Errorf("client error logged as internal failure: %s", logs.String())
				}
				return
			}
			var got api.Trip
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode trip: %v", err)
			}
			want := api.Trip{
				Id: fixture.ID, UserId: fixture.UserID, DriverId: fixture.DriverID,
				StartPoint: api.Coordinates{Latitude: fixture.StartLatitude, Longitude: fixture.StartLongitude},
				EndPoint:   api.Coordinates{Latitude: fixture.EndLatitude, Longitude: fixture.EndLongitude},
				Price:      fixture.Price, Status: api.Active, StartedAt: fixture.StartedAt,
			}
			if response.Header().Get("Content-Type") != "application/json" || !reflect.DeepEqual(got, want) {
				t.Errorf("JSON response = %+v, want %+v", got, want)
			}
		})
	}
}
