package create

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/model"
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

func (uc *UseCase) CreateTrip(ctx context.Context, input model.CreateTripInput) (trip.Trip, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)

	t := trip.Trip{
		ID: uuid.New(),

		UserID:   input.UserID,
		DriverID: input.DriverID,

		StartLatitude:  input.StartLatitude,
		StartLongitude: input.StartLongitude,
		EndLatitude:    input.EndLatitude,
		EndLongitude:   input.EndLongitude,

		Price:  input.Price,
		Status: trip.StatusActive,

		StartedAt: now,
	}

	err := uc.txManager.Do(ctx, func(ctx context.Context) error {
		if err := uc.repo.Create(ctx, t); err != nil {
			return err
		}

		return uc.repo.CreateStatusHistory(ctx, trip.StatusHistory{
			TripID:    t.ID,
			ToStatus:  trip.StatusActive,
			Reason:    "created",
			ChangedAt: now,
		})
	})
	if err != nil {
		return trip.Trip{}, err
	}

	uc.metrics.TripCreated(ctx)
	uc.logger.InfoContext(ctx, "trip created", slog.String("trip_id", t.ID.String()))
	return t, nil
}
