package getpositions_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/getpositions"
)

func TestHandler_ListTripPositions_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	useCase := NewMockuseCase(ctrl)
	tripID := uuid.New()

	useCase.EXPECT().
		ListTripPositions(gomock.Any(), tripID).
		Return(nil, trip.ErrTripNotFound)

	handler := getpositions.NewHandler(useCase, time.Second, slog.Default())
	path := "/api/v1/trips/" + tripID.String() + "/positions"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()

	handler.ListTripPositions(response, request, tripID)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s",
			response.Code, http.StatusNotFound, response.Body.String())
	}

	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem response: %v", err)
	}

	if problem.Code != "trip_not_found" {
		t.Errorf("problem code = %q, want %q", problem.Code, "trip_not_found")
	}
}

func TestHandler_ListTripPositions_OK(t *testing.T) {
	tripID := uuid.New()
	recordedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		positions []trip.Position
		want      []api.TripPosition
	}{
		{
			name: "empty route",
			want: []api.TripPosition{},
		},
		{
			name: "route with positions",
			positions: []trip.Position{
				{ID: 10, TripId: tripID, Latitude: 55.75, Longitude: 37.61, RecordedAt: recordedAt},
				{ID: 11, TripId: tripID, Latitude: 55.76, Longitude: 37.62, RecordedAt: recordedAt},
			},
			want: []api.TripPosition{
				{
					Id:          10,
					TripId:      tripID,
					Coordinates: api.Coordinates{Latitude: 55.75, Longitude: 37.61},
					RecordedAt:  recordedAt,
				},
				{
					Id:          11,
					TripId:      tripID,
					Coordinates: api.Coordinates{Latitude: 55.76, Longitude: 37.62},
					RecordedAt:  recordedAt,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			useCase := NewMockuseCase(ctrl)
			useCase.EXPECT().
				ListTripPositions(gomock.Any(), tripID).
				Return(tt.positions, nil)

			handler := getpositions.NewHandler(useCase, time.Second, slog.Default())
			path := "/api/v1/trips/" + tripID.String() + "/positions"
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			handler.ListTripPositions(response, request, tripID)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s",
					response.Code, http.StatusOK, response.Body.String())
			}

			var got api.TripPositionsResponse
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if !reflect.DeepEqual(got.Positions, tt.want) {
				t.Errorf("positions = %#v, want %#v", got.Positions, tt.want)
			}
		})
	}
}
