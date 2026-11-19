package model

import (
	"github.com/google/uuid"
	"time"
)

type CreateTripInput struct {
	UserID   uuid.UUID
	DriverID uuid.UUID

	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64

	Price int64
}

type SavePositionsInput struct {
	Latitude  float64
	Longitude float64

	RecordedAt time.Time
}

type StaleTrip struct {
	TripID         uuid.UUID
	DriverID       uuid.UUID
	LastActivityAt time.Time
}
