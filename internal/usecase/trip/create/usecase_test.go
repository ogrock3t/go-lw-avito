package create_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/model"
	"github.com/ogrock3t/go-lw-avito/internal/usecase/trip/create"
)

func TestUseCase_CreateTrip_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)

	input := model.CreateTripInput{
		UserID:         uuid.New(),
		DriverID:       uuid.New(),
		StartLatitude:  55.751244,
		StartLongitude: 37.618423,
		EndLatitude:    59.934280,
		EndLongitude:   30.335099,
		Price:          1500,
	}

	var savedTrip trip.Trip
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

	createCall := repo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, got trip.Trip) {
			savedTrip = got
		}).
		Return(nil)

	historyCall := repo.EXPECT().
		CreateStatusHistory(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, got trip.StatusHistory) {
			savedHistory = got
		}).
		Return(nil)

	gomock.InOrder(txCall, createCall, historyCall)

	var logs bytes.Buffer
	metrics := NewMocktripMetrics(ctrl)
	metrics.EXPECT().TripCreated(gomock.Any()).Do(func(context.Context) {
		if !committed {
			t.Error("created counter incremented before commit")
		}
	})
	useCase := create.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), metrics)

	created, err := useCase.CreateTrip(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateTrip() error = %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatalf("decode business event: %v", err)
	}
	if event["msg"] != "trip created" || event["trip_id"] != created.ID.String() || event["level"] != slog.LevelInfo.String() {
		t.Errorf("business event = %v", event)
	}

	if created.ID == uuid.Nil {
		t.Error("CreateTrip() returned trip with empty ID")
	}
	if created.UserID != input.UserID {
		t.Errorf("CreateTrip() UserID = %v, want %v", created.UserID, input.UserID)
	}
	if created.DriverID != input.DriverID {
		t.Errorf("CreateTrip() DriverID = %v, want %v", created.DriverID, input.DriverID)
	}
	if created.Status != trip.StatusActive {
		t.Errorf("CreateTrip() Status = %q, want %q", created.Status, trip.StatusActive)
	}
	if created.StartedAt.IsZero() {
		t.Error("CreateTrip() returned trip with zero StartedAt")
	}
	if created.FinishedAt != nil {
		t.Errorf("CreateTrip() FinishedAt = %v, want nil", created.FinishedAt)
	}
	if created.Price != input.Price {
		t.Errorf("CreateTrip() Price = %d, want %d", created.Price, input.Price)
	}

	if !reflect.DeepEqual(savedTrip, created) {
		t.Errorf("repository Create() trip = %+v, want %+v", savedTrip, created)
	}
	if savedHistory.TripID != created.ID {
		t.Errorf("history TripID = %v, want %v", savedHistory.TripID, created.ID)
	}
	if savedHistory.FromStatus != nil {
		t.Errorf("history FromStatus = %v, want nil", savedHistory.FromStatus)
	}
	if savedHistory.ToStatus != trip.StatusActive {
		t.Errorf("history ToStatus = %q, want %q", savedHistory.ToStatus, trip.StatusActive)
	}
	if savedHistory.Reason != "created" {
		t.Errorf("history Reason = %q, want %q", savedHistory.Reason, "created")
	}
	if !savedHistory.ChangedAt.Equal(created.StartedAt) {
		t.Errorf("history ChangedAt = %v, want %v", savedHistory.ChangedAt, created.StartedAt)
	}
}

func TestUseCase_CreateTrip_CommitFailureDoesNotLogSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)
	var logs bytes.Buffer
	commitErr := errors.New("commit failed")
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
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
	uc := create.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), NewMocktripMetrics(ctrl))
	_, err := uc.CreateTrip(context.Background(), model.CreateTripInput{})
	if !errors.Is(err, commitErr) || logs.Len() != 0 {
		t.Errorf("error = %v, logs = %s; want commit error without success event", err, logs.String())
	}
}

func TestUseCase_CreateTrip_DriverBusy(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)

	txCall := txManager.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	createCall := repo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		Return(trip.ErrDriverBusy)

	gomock.InOrder(txCall, createCall)

	var logs bytes.Buffer
	useCase := create.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), NewMocktripMetrics(ctrl))

	created, err := useCase.CreateTrip(context.Background(), model.CreateTripInput{
		UserID:   uuid.New(),
		DriverID: uuid.New(),
	})
	if !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("CreateTrip() error = %v, want %v", err, trip.ErrDriverBusy)
	}

	if !reflect.DeepEqual(created, trip.Trip{}) {
		t.Errorf("CreateTrip() trip = %+v, want zero value", created)
	}
	if logs.Len() != 0 {
		t.Errorf("failed creation logged success: %s", logs.String())
	}
}

func TestUseCase_CreateTrip_HistoryFailureDoesNotIncrementCounter(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)
	repositoryErr := errors.New("history insert failed")
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().CreateStatusHistory(gomock.Any(), gomock.Any()).Return(repositoryErr)
	txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	var logs bytes.Buffer
	uc := create.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), NewMocktripMetrics(ctrl))
	_, err := uc.CreateTrip(context.Background(), model.CreateTripInput{})
	if !errors.Is(err, repositoryErr) {
		t.Errorf("error = %v, want history insert error", err)
	}
}
