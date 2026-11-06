package get

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

func (uc *UseCase) GetTrip(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return uc.repo.GetByID(ctx, id)
}
