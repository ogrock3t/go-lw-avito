package trip

import (
	"github.com/google/uuid"
	"time"
)

type Position struct {
	ID         int64
	TripId     uuid.UUID
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
	CreatedAt  time.Time
}
