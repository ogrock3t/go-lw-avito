package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/trip"
)

type server struct {
	api.Unimplemented

	service      tripService
	pool         databasePool
	queryTimeout time.Duration
}

type createTripRequest struct {
	UserID     *uuid.UUID          `json:"user_id"`
	DriverID   *uuid.UUID          `json:"driver_id"`
	StartPoint *coordinatesRequest `json:"start_point"`
	EndPoint   *coordinatesRequest `json:"end_point"`
	Price      *int64              `json:"price"`
}

type coordinatesRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

func NewServer(service tripService, pool databasePool, queryTimeout time.Duration) *server {
	return &server{
		service:      service,
		pool:         pool,
		queryTimeout: queryTimeout,
	}
}

func (s *server) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	var body createTripRequest

	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "Invalid JSON body")
		return
	}

	if err := validateCreateTripRequest(body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.queryTimeout)
	defer cancel()

	created, err := s.service.CreateTrip(ctx, trip.CreateTripInput{
		UserID:         *body.UserID,
		DriverID:       *body.DriverID,
		StartLatitude:  *body.StartPoint.Latitude,
		StartLongitude: *body.StartPoint.Longitude,
		EndLatitude:    *body.EndPoint.Latitude,
		EndLongitude:   *body.EndPoint.Longitude,
		Price:          *body.Price,
	})
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+created.ID.String())
	writeJSON(w, http.StatusCreated, toAPITrip(created))
}

func (s *server) GetTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	ctx, cancel := context.WithTimeout(r.Context(), s.queryTimeout)
	defer cancel()

	t, err := s.service.GetTrip(ctx, uuid.UUID(tripId))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPITrip(t))
}

func (s *server) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	ctx, cancel := context.WithTimeout(r.Context(), s.queryTimeout)
	defer cancel()

	t, err := s.service.FinishTrip(ctx, uuid.UUID(tripId))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPITrip(t))
}

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

func validateCreateTripRequest(body createTripRequest) error {
	if body.UserID == nil {
		return errors.New("user_id is required")
	}

	if *body.UserID == uuid.Nil {
		return errors.New("user_id must be non-empty UUID")
	}

	if body.DriverID == nil {
		return errors.New("driver_id is required")
	}

	if *body.DriverID == uuid.Nil {
		return errors.New("driver_id must be non-empty UUID")
	}

	if body.StartPoint == nil {
		return errors.New("start_point is required")
	}

	if err := validateCoordinatesRequest("start_point", *body.StartPoint); err != nil {
		return err
	}

	if body.EndPoint == nil {
		return errors.New("end_point is required")
	}

	if err := validateCoordinatesRequest("end_point", *body.EndPoint); err != nil {
		return err
	}

	if body.Price == nil {
		return errors.New("price is required")
	}

	if *body.Price < 0 {
		return errors.New("price must be non-negative")
	}

	return nil
}

func validateCoordinatesRequest(name string, coordinates coordinatesRequest) error {
	if coordinates.Latitude == nil {
		return errors.New(name + ".latitude is required")
	}

	if !validLatitude(*coordinates.Latitude) {
		return errors.New(name + ".latitude must be between -90 and 90")
	}

	if coordinates.Longitude == nil {
		return errors.New(name + ".longitude is required")
	}

	if !validLongitude(*coordinates.Longitude) {
		return errors.New(name + ".longitude must be between -180 and 180")
	}

	return nil
}

func validLatitude(value float64) bool {
	return value >= -90 && value <= 90
}

func validLongitude(value float64) bool {
	return value >= -180 && value <= 180
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
