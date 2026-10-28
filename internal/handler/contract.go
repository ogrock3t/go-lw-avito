package handler

import (
	"context"

	"github.com/google/uuid"
	"github.com/ogrock3t/go-lw-avito/internal/trip"
)

type tripService interface {
	CreateTrip(ctx context.Context, input trip.CreateTripInput) (trip.Trip, error)
	GetTrip(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID) (trip.Trip, error)
}

type databasePool interface {
	Ping(ctx context.Context) error
}
