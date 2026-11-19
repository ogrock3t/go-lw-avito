//go:generate go tool mockgen -source=$GOFILE -destination=mocks_test.go -package=${GOPACKAGE}_test

package positions

import (
	"context"
	"github.com/google/uuid"
	"github.com/ogrock3t/go-lw-avito/internal/model"
	"time"
)

type tripRepository interface {
	ListStaleTrip(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error)
}

type pushGateway interface {
	RequestPosition(ctx context.Context, driverID uuid.UUID, tripID uuid.UUID) error
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}
