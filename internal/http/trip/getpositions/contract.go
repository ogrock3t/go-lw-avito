//go:generate go tool mockgen -source=$GOFILE -destination=mocks_test.go -package=${GOPACKAGE}_test

package getpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

type useCase interface {
	ListTripPositions(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error)
}
