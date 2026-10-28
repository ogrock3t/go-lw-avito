package trip

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type Service struct {
	repo      Repository
	txManager TxManager
}

func NewService(repo Repository, txManager TxManager) *Service {
	return &Service{
		repo:      repo,
		txManager: txManager,
	}
}

func (s *Service) CreateTrip(ctx context.Context, input CreateTripInput) (Trip, error) {
	now := time.Now().UTC()

	t := Trip{
		ID: uuid.New(),

		UserID:   input.UserID,
		DriverID: input.DriverID,

		StartLatitude:  input.StartLatitude,
		StartLongitude: input.StartLongitude,
		EndLatitude:    input.EndLatitude,
		EndLongitude:   input.EndLongitude,

		Price:  input.Price,
		Status: StatusActive,

		StartedAt: now,
	}

	err := s.txManager.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, t); err != nil {
			return err
		}

		return s.repo.CreateStatusHistory(ctx, StatusHistory{
			TripID:    t.ID,
			ToStatus:  StatusActive,
			Reason:    "created",
			ChangedAt: now,
		})
	})
	if err != nil {
		return Trip{}, err
	}

	return t, nil
}

func (s *Service) GetTrip(ctx context.Context, id uuid.UUID) (Trip, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) FinishTrip(ctx context.Context, id uuid.UUID) (Trip, error) {
	now := time.Now().UTC()

	var finished Trip

	err := s.txManager.Do(ctx, func(ctx context.Context) error {
		t, err := s.repo.Finish(ctx, id, now)
		if err != nil {
			return err
		}

		fromStatus := StatusActive

		if err := s.repo.CreateStatusHistory(ctx, StatusHistory{
			TripID:     t.ID,
			FromStatus: &fromStatus,
			ToStatus:   StatusCompleted,
			Reason:     "finished",
			ChangedAt:  now,
		}); err != nil {
			return err
		}

		finished = t
		return nil
	})
	if err != nil {
		return Trip{}, err
	}

	return finished, nil
}
