package getpositions

import (
	"context"
	"github.com/google/uuid"
	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

type UseCase struct {
	repo tripRepository
}

func NewUseCase(repo tripRepository) *UseCase {
	return &UseCase{
		repo: repo,
	}
}

func (uc *UseCase) ListTripPositions(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error) {
	if _, err := uc.repo.GetByID(ctx, tripID); err != nil {
		return nil, err
	}

	return uc.repo.ListPositions(ctx, tripID)
}
