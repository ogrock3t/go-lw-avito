package savepositions_test

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
	"github.com/ogrock3t/go-lw-avito/internal/model"
	"github.com/ogrock3t/go-lw-avito/internal/usecase/trip/savepositions"
)

func TestUseCase_SavePosition_SuccessAfterCommit(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txManager := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)
	canceler := NewMockpositionCanceler(ctrl)
	tripID := uuid.New()
	input := validPositionInput()
	position := trip.Position{
		TripId: tripID, Latitude: input.Latitude, Longitude: input.Longitude, RecordedAt: input.RecordedAt,
	}
	saved := position
	saved.ID = 42
	saved.RecordedAt = input.RecordedAt.Truncate(time.Microsecond)
	saved.CreatedAt = saved.RecordedAt.Add(time.Second)
	ctx := context.Background()
	transactionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	committed := false
	var logs bytes.Buffer

	txCall := txManager.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, fn func(context.Context) error) error {
			if err := fn(transactionCtx); err != nil {
				return err
			}
			if logs.Len() != 0 {
				t.Error("position success logged before commit")
			}
			committed = true
			return nil
		},
	)
	gomock.InOrder(
		txCall,
		repo.EXPECT().GetByIDForUpdate(transactionCtx, tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusActive}, nil),
		repo.EXPECT().InsertPositions(transactionCtx, position).Return(saved, nil),
		repo.EXPECT().UpdateLastPosition(transactionCtx, tripID, saved.RecordedAt).Return(nil),
	)
	canceler.EXPECT().CancelTrip(tripID).Do(func(uuid.UUID) {
		if !committed {
			t.Error("position request canceled before commit")
		}
	})
	metrics.EXPECT().PositionSaved(ctx).Do(func(context.Context) {
		if !committed {
			t.Error("saved counter incremented before commit")
		}
	})
	uc := savepositions.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)), metrics, canceler)

	got, err := uc.SavePosition(ctx, tripID, input)
	if err != nil {
		t.Fatalf("SavePosition() error = %v", err)
	}
	if !reflect.DeepEqual(got, saved) {
		t.Errorf("SavePosition() = %+v, want %+v", got, saved)
	}
	var event map[string]any
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatalf("decode position event: %v", err)
	}
	if event["msg"] != "trip position saved" || event["trip_id"] != tripID.String() ||
		event["position_id"] != float64(saved.ID) || event["level"] != slog.LevelInfo.String() {
		t.Errorf("position event = %v", event)
	}
}

func TestUseCase_SavePosition_RepositoryErrors(t *testing.T) {
	t.Parallel()
	repositoryErr := errors.New("repository operation failed")
	tests := []struct {
		name    string
		setup   func(*MocktripRepository, uuid.UUID, time.Time)
		wantErr error
	}{
		{
			name: "trip not found",
			setup: func(repo *MocktripRepository, tripID uuid.UUID, _ time.Time) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "trip lookup failed",
			setup: func(repo *MocktripRepository, tripID uuid.UUID, _ time.Time) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{}, repositoryErr)
			},
			wantErr: repositoryErr,
		},
		{
			name: "trip completed",
			setup: func(repo *MocktripRepository, tripID uuid.UUID, _ time.Time) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusCompleted}, nil)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "position insert failed",
			setup: func(repo *MocktripRepository, tripID uuid.UUID, _ time.Time) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusActive}, nil)
				repo.EXPECT().InsertPositions(gomock.Any(), gomock.Any()).Return(trip.Position{}, repositoryErr)
			},
			wantErr: repositoryErr,
		},
		{
			name: "last position update failed",
			setup: func(repo *MocktripRepository, tripID uuid.UUID, recordedAt time.Time) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusActive}, nil)
				repo.EXPECT().InsertPositions(gomock.Any(), gomock.Any()).Return(trip.Position{TripId: tripID, RecordedAt: recordedAt}, nil)
				repo.EXPECT().UpdateLastPosition(gomock.Any(), tripID, recordedAt).Return(repositoryErr)
			},
			wantErr: repositoryErr,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			txManager := NewMocktxManager(ctrl)
			tripID := uuid.New()
			input := validPositionInput()
			test.setup(repo, tripID, input.RecordedAt)
			txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
			)
			var logs bytes.Buffer
			uc := savepositions.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)),
				NewMocktripMetrics(ctrl), NewMockpositionCanceler(ctrl))

			got, err := uc.SavePosition(context.Background(), tripID, input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("SavePosition() error = %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(got, trip.Position{}) {
				t.Errorf("SavePosition() = %+v, want zero value on failure", got)
			}
			if logs.Len() != 0 {
				t.Errorf("failed save logged success: %s", logs.String())
			}
		})
	}
}

func TestUseCase_SavePosition_TransactionErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		executeCallback bool
	}{
		{name: "begin failed"},
		{name: "commit failed", executeCallback: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			txManager := NewMocktxManager(ctrl)
			tripID := uuid.New()
			input := validPositionInput()
			transactionErr := errors.New(test.name)
			if test.executeCallback {
				gomock.InOrder(
					repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(trip.Trip{ID: tripID, Status: trip.StatusActive}, nil),
					repo.EXPECT().InsertPositions(gomock.Any(), gomock.Any()).Return(trip.Position{ID: 42, TripId: tripID, RecordedAt: input.RecordedAt}, nil),
					repo.EXPECT().UpdateLastPosition(gomock.Any(), tripID, input.RecordedAt).Return(nil),
				)
			}
			txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, fn func(context.Context) error) error {
					if test.executeCallback {
						if err := fn(ctx); err != nil {
							return err
						}
					}
					return transactionErr
				},
			)
			var logs bytes.Buffer
			uc := savepositions.NewUseCase(repo, txManager, slog.New(slog.NewJSONHandler(&logs, nil)),
				NewMocktripMetrics(ctrl), NewMockpositionCanceler(ctrl))

			got, err := uc.SavePosition(context.Background(), tripID, input)
			if !errors.Is(err, transactionErr) {
				t.Fatalf("SavePosition() error = %v, want %v", err, transactionErr)
			}
			if !reflect.DeepEqual(got, trip.Position{}) {
				t.Errorf("SavePosition() = %+v, want zero value on transaction failure", got)
			}
			if logs.Len() != 0 {
				t.Errorf("failed transaction logged success: %s", logs.String())
			}
		})
	}
}

func validPositionInput() model.SavePositionsInput {
	return model.SavePositionsInput{
		Latitude:   55.75,
		Longitude:  37.61,
		RecordedAt: time.Date(2026, 1, 1, 12, 0, 0, 123456789, time.UTC),
	}
}
