package positions

import (
	"github.com/google/uuid"
	"time"
)

type task struct {
	id             uuid.UUID
	tripID         uuid.UUID
	driverID       uuid.UUID
	lastActivityAt time.Time
}
