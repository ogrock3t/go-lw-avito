package trip

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

type Trip struct {
	ID uuid.UUID

	UserID   uuid.UUID
	DriverID uuid.UUID

	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64

	Price  int64
	Status Status

	StartedAt  time.Time
	FinishedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

type StatusHistory struct {
	TripID uuid.UUID

	FromStatus *Status
	ToStatus   Status

	Reason    string
	ChangedAt time.Time
}

type CreateTripInput struct {
	UserID   uuid.UUID
	DriverID uuid.UUID

	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64

	Price int64
}

var (
	ErrTripNotFound  = errors.New("trip not found")
	ErrDriverBusy    = errors.New("driver busy")
	ErrTripCompleted = errors.New("trip completed")
)
