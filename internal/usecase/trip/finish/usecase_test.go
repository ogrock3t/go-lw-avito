package finish_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/usecase/trip/finish"
)

func TestUseCase_FinishTrip_ActiveTripSuccess(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)

	tripID := uuid.New()
	activeTrip := trip.Trip{
		ID:        tripID,
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Price:     1500,
		Status:    trip.StatusActive,
		StartedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
	}

	var updatedTrip trip.Trip
	var savedHistory trip.StatusHistory
	committed := false

	txCall := txManager.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			if err := fn(ctx); err != nil {
				return err
			}
			committed = true
			return nil
		})

	getCall := repo.EXPECT().
		GetByIDForUpdate(gomock.Any(), tripID).
		Return(activeTrip, nil)

	updateCall := repo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, got trip.Trip) {
			updatedTrip = got
		}).
		Return(nil)

	historyCall := repo.EXPECT().
		CreateStatusHistory(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, got trip.StatusHistory) {
			savedHistory = got
		}).
		Return(nil)

	gomock.InOrder(txCall, getCall, updateCall, historyCall)

	var logs bytes.Buffer
	metrics := NewMocktripMetrics(ctrl)
	metrics.EXPECT().TripCompleted(gomock.Any()).Do(func(context.Context) {
		if !committed {
			t.Error("completed counter incremented before commit")
		}
	})
	useCase := finish.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), metrics)

	finished, err := useCase.FinishTrip(context.Background(), tripID)
	if err != nil {
		t.Fatalf("FinishTrip() error = %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatalf("decode business event: %v", err)
	}
	if event["msg"] != "trip completed" || event["trip_id"] != tripID.String() || event["level"] != slog.LevelInfo.String() {
		t.Errorf("business event = %v", event)
	}

	if finished.ID != activeTrip.ID {
		t.Errorf("FinishTrip() ID = %v, want %v", finished.ID, activeTrip.ID)
	}
	if finished.Status != trip.StatusCompleted {
		t.Errorf("FinishTrip() Status = %q, want %q", finished.Status, trip.StatusCompleted)
	}
	if finished.FinishedAt == nil {
		t.Fatal("FinishTrip() FinishedAt = nil, want completion time")
	}
	if !finished.UpdatedAt.Equal(*finished.FinishedAt) {
		t.Errorf("FinishTrip() UpdatedAt = %v, want %v", finished.UpdatedAt, *finished.FinishedAt)
	}
	if !reflect.DeepEqual(updatedTrip, finished) {
		t.Errorf("repository Update() trip = %+v, want %+v", updatedTrip, finished)
	}

	if savedHistory.TripID != finished.ID {
		t.Errorf("history TripID = %v, want %v", savedHistory.TripID, finished.ID)
	}
	if savedHistory.FromStatus == nil || *savedHistory.FromStatus != trip.StatusActive {
		t.Errorf("history FromStatus = %v, want %q", savedHistory.FromStatus, trip.StatusActive)
	}
	if savedHistory.ToStatus != trip.StatusCompleted {
		t.Errorf("history ToStatus = %q, want %q", savedHistory.ToStatus, trip.StatusCompleted)
	}
	if savedHistory.Reason != "finished" {
		t.Errorf("history Reason = %q, want %q", savedHistory.Reason, "finished")
	}
	if !savedHistory.ChangedAt.Equal(*finished.FinishedAt) {
		t.Errorf("history ChangedAt = %v, want %v", savedHistory.ChangedAt, *finished.FinishedAt)
	}
}

func TestUseCase_FinishTrip_CommitFailureDoesNotLogSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)
	var logs bytes.Buffer
	commitErr := errors.New("commit failed")
	tripID := uuid.New()
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusActive}, nil)
	repo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().CreateStatusHistory(gomock.Any(), gomock.Any()).Return(nil)
	txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			if err := fn(ctx); err != nil {
				t.Fatalf("transaction callback: %v", err)
			}
			if logs.Len() != 0 {
				t.Error("success logged before commit")
			}
			return commitErr
		},
	)
	uc := finish.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), NewMocktripMetrics(ctrl))
	_, err := uc.FinishTrip(context.Background(), tripID)
	if !errors.Is(err, commitErr) || logs.Len() != 0 {
		t.Errorf("error = %v, logs = %s; want commit error without success event", err, logs.String())
	}
}

func TestUseCase_FinishTrip_CompletedTrip(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)

	tripID := uuid.New()
	finishedAt := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	completedTrip := trip.Trip{
		ID:         tripID,
		Status:     trip.StatusCompleted,
		StartedAt:  finishedAt.Add(-time.Hour),
		FinishedAt: &finishedAt,
		UpdatedAt:  finishedAt,
	}

	txCall := txManager.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	getCall := repo.EXPECT().
		GetByIDForUpdate(gomock.Any(), tripID).
		Return(completedTrip, nil)

	gomock.InOrder(txCall, getCall)

	var logs bytes.Buffer
	useCase := finish.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), NewMocktripMetrics(ctrl))

	finished, err := useCase.FinishTrip(context.Background(), tripID)
	if !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("FinishTrip() error = %v, want %v", err, trip.ErrTripCompleted)
	}

	if !reflect.DeepEqual(finished, trip.Trip{}) {
		t.Errorf("FinishTrip() trip = %+v, want zero value", finished)
	}
	if logs.Len() != 0 {
		t.Errorf("failed completion logged success: %s", logs.String())
	}
}

func TestUseCase_FinishTrip_HistoryFailureDoesNotIncrementCounter(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)
	tripID := uuid.New()
	repositoryErr := errors.New("history insert failed")
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusActive}, nil)
	repo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().CreateStatusHistory(gomock.Any(), gomock.Any()).Return(repositoryErr)
	txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	var logs bytes.Buffer
	uc := finish.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), NewMocktripMetrics(ctrl))
	_, err := uc.FinishTrip(context.Background(), tripID)
	if !errors.Is(err, repositoryErr) {
		t.Errorf("error = %v, want history insert error", err)
	}
}
