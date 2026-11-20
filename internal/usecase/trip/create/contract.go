//go:generate go tool mockgen -source=$GOFILE -destination=mocks_test.go -package=${GOPACKAGE}_test

package create

import (
	"context"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

type tripRepository interface {
	Create(ctx context.Context, trip trip.Trip) error
	CreateStatusHistory(ctx context.Context, history trip.StatusHistory) error
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type tripMetrics interface {
	TripCreated(ctx context.Context)
}
