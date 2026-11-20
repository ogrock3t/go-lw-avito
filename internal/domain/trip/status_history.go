package trip

import (
	"time"

	"github.com/google/uuid"
)

type StatusHistory struct {
	TripID uuid.UUID

	FromStatus *Status
	ToStatus   Status

	Reason    string
	ChangedAt time.Time
}
