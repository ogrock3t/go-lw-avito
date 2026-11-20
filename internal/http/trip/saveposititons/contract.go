package saveposititons

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/model"
)

type savePositionRequest struct {
	Coordinates *coordinatesRequest `json:"coordinates"`
	RecordedAt  *time.Time          `json:"recorded_at"`
}

type coordinatesRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type useCase interface {
	SavePosition(ctx context.Context, tripID uuid.UUID, input model.SavePositionsInput) (trip.Position, error)
}
