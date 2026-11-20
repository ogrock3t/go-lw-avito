package positions

import (
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type task struct {
	id             uuid.UUID
	tripID         uuid.UUID
	driverID       uuid.UUID
	lastActivityAt time.Time
	scanContext    trace.SpanContext
}
