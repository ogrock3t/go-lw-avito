package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/trip"
)

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
