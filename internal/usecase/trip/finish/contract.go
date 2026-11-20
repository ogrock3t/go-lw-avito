//go:generate go tool mockgen -source=$GOFILE -destination=mocks_test.go -package=${GOPACKAGE}_test

package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

type tripRepository interface {
	CreateStatusHistory(ctx context.Context, history trip.StatusHistory) error
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	Update(ctx context.Context, trip trip.Trip) error
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type tripMetrics interface {
	TripCompleted(ctx context.Context)
}
