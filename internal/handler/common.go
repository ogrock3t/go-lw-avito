package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/trip"
)

func (s *server) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{
		Status: api.Ok,
	})
}

func (s *server) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.queryTimeout)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{
			Status: api.Unavailable,
		})
		return
	}

	writeJSON(w, http.StatusOK, api.HealthResponse{
		Status: api.Ok,
	})
}

func (s *server) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, trip.ErrTripNotFound):
		writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip not found")
	case errors.Is(err, trip.ErrDriverBusy):
		writeProblem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrTripCompleted):
		writeProblem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "Trip already completed")
	default:
		slog.Error("handle request", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
	}
}

func GeneratedErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "Invalid path or header parameter")
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("body must contain single JSON object")
	}

	return nil
}

func toAPITrip(t trip.Trip) api.Trip {
	return api.Trip{
		Id:         t.ID,
		UserId:     t.UserID,
		DriverId:   t.DriverID,
		StartPoint: api.Coordinates{Latitude: t.StartLatitude, Longitude: t.StartLongitude},
		EndPoint:   api.Coordinates{Latitude: t.EndLatitude, Longitude: t.EndLongitude},
		Price:      t.Price,
		Status:     api.TripStatus(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response", "error", err)
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, statusCode int, code string, title string, detail string) {
	instance := r.URL.Path

	writeProblemBody(w, statusCode, api.Problem{
		Type:     "https://tripgo.example/problems/" + code,
		Title:    title,
		Status:   int32(statusCode),
		Detail:   &detail,
		Instance: &instance,
		Code:     code,
	})
}

func writeProblemBody(w http.ResponseWriter, statusCode int, problem api.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(problem); err != nil {
		slog.Error("write problem response", "error", err)
	}
}
