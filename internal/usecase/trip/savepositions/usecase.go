package savepositions

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/model"
)

type UseCase struct {
	repo             tripRepository
	txManager        txManager
	logger           *slog.Logger
	metrics          tripMetrics
	positionCanceler positionCanceler
}

func NewUseCase(repo tripRepository, txManager txManager, logger *slog.Logger, metrics tripMetrics, positionCanceler positionCanceler) *UseCase {
	return &UseCase{
		repo:             repo,
		txManager:        txManager,
		logger:           logger,
		metrics:          metrics,
		positionCanceler: positionCanceler,
	}
}

func (uc *UseCase) SavePosition(ctx context.Context, tripID uuid.UUID, input model.SavePositionsInput) (trip.Position, error) {
	var saved trip.Position

	err := uc.txManager.Do(ctx, func(ctx context.Context) error {
		currentTrip, err := uc.repo.GetByIDForUpdate(ctx, tripID)
		if err != nil {
			return err
		}

		if currentTrip.Status != trip.StatusActive {
			return trip.ErrTripCompleted
		}

		position, err := uc.repo.InsertPositions(ctx, trip.Position{
			TripId:     tripID,
			Latitude:   input.Latitude,
			Longitude:  input.Longitude,
			RecordedAt: input.RecordedAt,
		})
		if err != nil {
			return err
		}

		if err := uc.repo.UpdateLastPosition(ctx, currentTrip.ID, position.RecordedAt); err != nil {
			return err
		}

		saved = position

		return nil
	})
	if err != nil {
		return trip.Position{}, err
	}

	uc.positionCanceler.CancelTrip(tripID)

	uc.metrics.PositionSaved(ctx)
	uc.logger.InfoContext(ctx, "trip position saved",
		slog.String("trip_id", saved.TripId.String()),
		slog.Int64("position_id", saved.ID),
	)

	return saved, nil
}
