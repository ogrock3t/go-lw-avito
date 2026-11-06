package finish

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

type UseCase struct {
	repo      tripRepository
	txManager txManager
	logger    *slog.Logger
	metrics   tripMetrics
}

func NewUseCase(repo tripRepository, txManager txManager, logger *slog.Logger, metrics tripMetrics) *UseCase {
	return &UseCase{
		repo:      repo,
		txManager: txManager,
		logger:    logger,
		metrics:   metrics,
	}
}

func (uc *UseCase) FinishTrip(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	now := time.Now().UTC()

	var finished trip.Trip

	err := uc.txManager.Do(ctx, func(ctx context.Context) error {
		t, err := uc.repo.GetByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}

		if err := t.Finish(now); err != nil {
			return err
		}

		if err := uc.repo.Update(ctx, t); err != nil {
			return err
		}

		fromStatus := trip.StatusActive

		if err := uc.repo.CreateStatusHistory(ctx, trip.StatusHistory{
			TripID:     t.ID,
			FromStatus: &fromStatus,
			ToStatus:   trip.StatusCompleted,
			Reason:     "finished",
			ChangedAt:  now,
		}); err != nil {
			return err
		}

		finished = t

		return nil
	})
	if err != nil {
		return trip.Trip{}, err
	}

	uc.metrics.TripCompleted(ctx)
	uc.logger.InfoContext(ctx, "trip completed", slog.String("trip_id", finished.ID.String()))
	return finished, nil
}
