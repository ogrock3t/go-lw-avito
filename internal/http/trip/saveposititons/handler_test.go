//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=saveposititons_test

package saveposititons_test

import (
	"encoding/json"
	"fmt"
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
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/saveposititons"
	"github.com/ogrock3t/go-lw-avito/internal/model"
)

const validPositionBody = `{"coordinates":{"latitude":55.75,"longitude":37.61},"recorded_at":"2026-01-01T12:00:00Z"}`

func TestHandler_CreateTripPosition_Created(t *testing.T) {
	tripID := uuid.New()
	recordedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	input := model.SavePositionsInput{
		Latitude:   55.75,
		Longitude:  37.61,
		RecordedAt: recordedAt,
	}

	saved := trip.Position{
		ID:         42,
		TripId:     tripID,
		Latitude:   input.Latitude,
		Longitude:  input.Longitude,
		RecordedAt: recordedAt,
	}

	wantPosition := api.TripPosition{
		Id:          42,
		TripId:      tripID,
		Coordinates: api.Coordinates{Latitude: 55.75, Longitude: 37.61},
		RecordedAt:  recordedAt,
	}

	useCase := NewMockuseCase(gomock.NewController(t))
	useCase.EXPECT().
		SavePosition(gomock.Any(), tripID, input).
		Return(saved, nil)

	response := postPosition(useCase, tripID, validPositionBody)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s",
			response.Code, http.StatusCreated, response.Body.String())
	}

	var got api.TripPosition
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode position response: %v", err)
	}
	if !reflect.DeepEqual(got, wantPosition) {
		t.Errorf("position = %+v, want %+v", got, wantPosition)
	}
}

func TestHandler_CreateTripPosition_InvalidRequest(t *testing.T) {
	futureBody := fmt.Sprintf(
		`{"coordinates":{"latitude":55.75,"longitude":37.61},"recorded_at":%q}`,
		time.Now().UTC().Add(10*time.Minute).Format(time.RFC3339),
	)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "latitude out of range",
			body: `{"coordinates":{"latitude":91,"longitude":37.61},"recorded_at":"2026-01-01T12:00:00Z"}`,
		},
		{
			name: "longitude out of range",
			body: `{"coordinates":{"latitude":55.75,"longitude":181},"recorded_at":"2026-01-01T12:00:00Z"}`,
		},
		{
			name: "invalid recorded_at",
			body: `{"coordinates":{"latitude":55.75,"longitude":37.61},"recorded_at":"yesterday"}`,
		},
		{
			name: "recorded_at in the future",
			body: futureBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCase := NewMockuseCase(gomock.NewController(t))
			response := postPosition(useCase, uuid.New(), tt.body)

			assertPositionProblem(t, response, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestHandler_CreateTripPosition_TripErrors(t *testing.T) {
	tripID := uuid.New()

	input := model.SavePositionsInput{
		Latitude:   55.75,
		Longitude:  37.61,
		RecordedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name     string
		saveErr  error
		wantHTTP int
		wantCode string
	}{
		{
			name:     "trip not found",
			saveErr:  trip.ErrTripNotFound,
			wantHTTP: http.StatusNotFound,
			wantCode: "trip_not_found",
		},
		{
			name:     "trip completed",
			saveErr:  trip.ErrTripCompleted,
			wantHTTP: http.StatusConflict,
			wantCode: "trip_completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCase := NewMockuseCase(gomock.NewController(t))
			useCase.EXPECT().
				SavePosition(gomock.Any(), tripID, input).
				Return(trip.Position{}, tt.saveErr)

			response := postPosition(useCase, tripID, validPositionBody)

			assertPositionProblem(t, response, tt.wantHTTP, tt.wantCode)
		})
	}
}

func postPosition(useCase *MockuseCase, tripID uuid.UUID, body string) *httptest.ResponseRecorder {
	handler := saveposititons.NewHandler(useCase, time.Second, slog.Default())
	path := "/api/v1/trips/" + tripID.String() + "/positions"
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.CreateTripPosition(response, request, tripID)

	return response
}

func assertPositionProblem(t *testing.T, response *httptest.ResponseRecorder, wantHTTP int, wantCode string) {
	t.Helper()

	if response.Code != wantHTTP {
		t.Fatalf("status = %d, want %d, body = %s",
			response.Code, wantHTTP, response.Body.String())
	}

	var problem api.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem response: %v", err)
	}
	if problem.Code != wantCode {
		t.Errorf("problem code = %q, want %q", problem.Code, wantCode)
	}
}
