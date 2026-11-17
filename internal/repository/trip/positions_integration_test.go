//go:build integration

//go:generate go tool mockgen -source=../../usecase/trip/savepositions/contract.go -exclude_interfaces=tripMetrics,txManager -destination=positions_mocks_test.go -package=trip_test -build_constraint=integration

package trip_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres/transaction"
	"github.com/ogrock3t/go-lw-avito/internal/infra/telemetry"
	"github.com/ogrock3t/go-lw-avito/internal/model"
	"github.com/ogrock3t/go-lw-avito/internal/usecase/trip/savepositions"
)

func TestSavePosition_PersistsLatePositionWithoutRewindingLastPositionAt(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)
	activeTrip := newActiveTrip()
	activeTrip.StartedAt = activeTrip.StartedAt.Add(-10 * time.Minute)
	cleanupTrips(t, pool, activeTrip.ID)
	insertTrip(t, ctx, pool, activeTrip)

	useCase := savepositions.NewUseCase(repository, transaction.NewTxManager(pool), slog.Default(), newPositionTestMetrics(t))
	input := model.SavePositionsInput{
		Latitude:   55.75,
		Longitude:  37.61,
		RecordedAt: activeTrip.StartedAt.Add(9 * time.Minute),
	}

	fresh, err := useCase.SavePosition(ctx, activeTrip.ID, input)
	if err != nil {
		t.Fatalf("save fresh position: %v", err)
	}
	freshAt := input.RecordedAt
	assertLastPositionAt(t, ctx, pool, activeTrip.ID, &freshAt)

	input.RecordedAt = freshAt.Add(-5 * time.Minute)
	late, err := useCase.SavePosition(ctx, activeTrip.ID, input)
	if err != nil {
		t.Fatalf("save late position: %v", err)
	}
	assertLastPositionAt(t, ctx, pool, activeTrip.ID, &freshAt)

	positions, err := repository.ListPositions(ctx, activeTrip.ID)
	if err != nil {
		t.Fatalf("list saved positions: %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("saved positions = %d, want 2", len(positions))
	}
	if positions[0].ID != late.ID || positions[1].ID != fresh.ID ||
		!positions[0].RecordedAt.Equal(input.RecordedAt) || !positions[1].RecordedAt.Equal(freshAt) {
		t.Errorf("positions = %+v, want late position %d at %v before fresh position %d at %v",
			positions, late.ID, input.RecordedAt, fresh.ID, freshAt)
	}
}

func TestSavePosition_RollsBackWhenUpdateLastPositionFails(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)
	activeTrip := newActiveTrip()
	cleanupTrips(t, pool, activeTrip.ID)
	insertTrip(t, ctx, pool, activeTrip)

	updateErr := errors.New("forced update last_position_at error")
	failingRepository := NewMocktripRepository(gomock.NewController(t))
	failingRepository.EXPECT().
		GetByIDForUpdate(gomock.Any(), activeTrip.ID).
		DoAndReturn(repository.GetByIDForUpdate)
	failingRepository.EXPECT().
		InsertPositions(gomock.Any(), gomock.Any()).
		DoAndReturn(repository.InsertPositions)
	failingRepository.EXPECT().
		UpdateLastPosition(gomock.Any(), activeTrip.ID, gomock.Any()).
		DoAndReturn(func(ctx context.Context, tripID uuid.UUID, _ time.Time) error {
			executor := transaction.ExecutorFromContext(ctx, pool)
			var count int
			if err := executor.QueryRow(ctx, countTripPositions, tripID).Scan(&count); err != nil {
				return fmt.Errorf("count positions inside transaction: %w", err)
			}
			if count != 1 {
				return fmt.Errorf("positions inside transaction = %d, want 1 before forcing the update error", count)
			}

			return updateErr
		})
	useCase := savepositions.NewUseCase(
		failingRepository, transaction.NewTxManager(pool), slog.Default(), newPositionTestMetrics(t),
	)

	_, err := useCase.SavePosition(ctx, activeTrip.ID, model.SavePositionsInput{
		Latitude:   55.75,
		Longitude:  37.61,
		RecordedAt: activeTrip.StartedAt,
	})
	if !errors.Is(err, updateErr) {
		t.Fatalf("SavePosition() error = %v, want %v", err, updateErr)
	}

	assertRowCount(t, ctx, pool, "trip_positions", countTripPositions, activeTrip.ID, 0)
	assertLastPositionAt(t, ctx, pool, activeTrip.ID, nil)
}

const countTripPositions = "SELECT count(*) FROM trip_positions WHERE trip_id = $1"

func assertLastPositionAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tripID uuid.UUID, want *time.Time) {
	t.Helper()

	var got *time.Time
	if err := pool.QueryRow(ctx, "SELECT last_position_at FROM trips WHERE id = $1", tripID).Scan(&got); err != nil {
		t.Fatalf("read last_position_at: %v", err)
	}
	assertOptionalTime(t, "trips.last_position_at", got, want)
}

func newPositionTestMetrics(t *testing.T) *telemetry.TripMetrics {
	t.Helper()

	metrics, err := telemetry.NewTripMetrics(noop.NewMeterProvider())
	if err != nil {
		t.Fatalf("create trip metrics: %v", err)
	}
	return metrics
}
