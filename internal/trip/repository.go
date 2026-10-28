package trip

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, trip Trip) error
	CreateStatusHistory(ctx context.Context, history StatusHistory) error
	GetByID(ctx context.Context, id uuid.UUID) (Trip, error)
	Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (Trip, error)
}