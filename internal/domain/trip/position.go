package trip

import (
	"time"

	"github.com/google/uuid"
)

type Position struct {
	ID         int64
	TripId     uuid.UUID
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
	CreatedAt  time.Time
}
