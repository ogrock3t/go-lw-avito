//go:generate go tool mockgen -source=$GOFILE -destination=mocks_test.go -package=${GOPACKAGE}_test

package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/model"
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

type useCase interface {
	CreateTrip(ctx context.Context, input model.CreateTripInput) (trip.Trip, error)
}
