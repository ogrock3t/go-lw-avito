//go:generate go tool mockgen -source=$GOFILE -destination=mocks_test.go -package=${GOPACKAGE}_test

package savepositions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

type tripRepository interface {
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	InsertPositions(ctx context.Context, position trip.Position) (trip.Position, error)
	UpdateLastPosition(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type tripMetrics interface {
	PositionSaved(ctx context.Context)
}

type positionCanceler interface {
	CancelTrip(tripID uuid.UUID)
}
