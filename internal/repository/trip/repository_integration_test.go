//go:build integration

package trip_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domaintrip "github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	triprepo "github.com/ogrock3t/go-lw-avito/internal/repository/trip"
)

func TestRepository_Create(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)
	createdTrip := newActiveTrip()
	cleanupTrips(t, pool, createdTrip.ID)

	if err := repository.Create(ctx, createdTrip); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	got, err := repository.GetByID(ctx, createdTrip.ID)
	if err != nil {
		t.Fatalf("GetByID() after Create() error = %v, want nil", err)
	}

	assertTrip(t, got, createdTrip)
}

func TestRepository_GetByID(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)
	want := newActiveTrip()
	want.CreatedAt = want.StartedAt.Add(-time.Minute)
	want.UpdatedAt = want.StartedAt.Add(time.Minute)
	cleanupTrips(t, pool, want.ID)
	insertTrip(t, ctx, pool, want)

	got, err := repository.GetByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v, want nil", err)
	}

	assertTrip(t, got, want)
}

func TestRepository_UpdateCompletesTrip(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)
	want := newActiveTrip()
	want.CreatedAt = want.StartedAt.Add(-time.Minute)
	want.UpdatedAt = want.StartedAt
	cleanupTrips(t, pool, want.ID)
	insertTrip(t, ctx, pool, want)

	finishedAt := want.StartedAt.Add(30 * time.Minute)
	if err := want.Finish(finishedAt); err != nil {
		t.Fatalf("Finish() error = %v, want nil", err)
	}
	if err := repository.Update(ctx, want); err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}

	got, err := repository.GetByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetByID() after Update() error = %v, want nil", err)
	}

	assertTrip(t, got, want)
}

func TestRepository_ErrTripNotFound(t *testing.T) {
	ctx, _, repository := newTestRepository(t)
	missingID := uuid.New()

	t.Run("get by id", func(t *testing.T) {
		_, err := repository.GetByID(ctx, missingID)
		if !errors.Is(err, domaintrip.ErrTripNotFound) {
			t.Fatalf("GetByID() error = %v, want %v", err, domaintrip.ErrTripNotFound)
		}
	})

	t.Run("update", func(t *testing.T) {
		missingTrip := newActiveTrip()
		missingTrip.ID = missingID

		err := repository.Update(ctx, missingTrip)
		if !errors.Is(err, domaintrip.ErrTripNotFound) {
			t.Fatalf("Update() error = %v, want %v", err, domaintrip.ErrTripNotFound)
		}
	})
}

func TestRepository_CreateReturnsErrDriverBusy(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)
	driverID := uuid.New()
	firstTrip := newActiveTrip()
	firstTrip.DriverID = driverID
	secondTrip := newActiveTrip()
	secondTrip.DriverID = driverID
	cleanupTrips(t, pool, firstTrip.ID, secondTrip.ID)

	if err := repository.Create(ctx, firstTrip); err != nil {
		t.Fatalf("first Create() error = %v, want nil", err)
	}

	err := repository.Create(ctx, secondTrip)
	if !errors.Is(err, domaintrip.ErrDriverBusy) {
		t.Fatalf("second Create() error = %v, want %v", err, domaintrip.ErrDriverBusy)
	}

	assertRowCount(t, ctx, pool, "trips", "SELECT count(*) FROM trips WHERE driver_id = $1", driverID, 1)
}

func newTestRepository(t *testing.T) (context.Context, *pgxpool.Pool, *triprepo.Repository) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	return ctx, pool, triprepo.NewRepository(pool)
}

func newActiveTrip() domaintrip.Trip {
	now := time.Now().UTC().Truncate(time.Microsecond)

	return domaintrip.Trip{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		DriverID:       uuid.New(),
		StartLatitude:  55.751244,
		StartLongitude: 37.618423,
		EndLatitude:    59.934280,
		EndLongitude:   30.335099,
		Price:          1500,
		Status:         domaintrip.StatusActive,
		StartedAt:      now,
	}
}

func insertTrip(t *testing.T, ctx context.Context, pool *pgxpool.Pool, trip domaintrip.Trip) {
	t.Helper()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO trips (
			id, user_id, driver_id,
			start_latitude, start_longitude, end_latitude, end_longitude,
			price, status, started_at, finished_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		trip.ID,
		trip.UserID,
		trip.DriverID,
		trip.StartLatitude,
		trip.StartLongitude,
		trip.EndLatitude,
		trip.EndLongitude,
		trip.Price,
		trip.Status,
		trip.StartedAt,
		trip.FinishedAt,
		trip.CreatedAt,
		trip.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("insert trip fixture: %v", err)
	}
}

func cleanupTrips(t *testing.T, pool *pgxpool.Pool, tripIDs ...uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := pool.Exec(ctx, "DELETE FROM trips WHERE id = ANY($1)", tripIDs); err != nil {
			t.Errorf("cleanup trips: %v", err)
		}
	})
}

func assertTrip(t *testing.T, got, want domaintrip.Trip) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("Trip.ID = %s, want %s", got.ID, want.ID)
	}
	if got.UserID != want.UserID {
		t.Errorf("Trip.UserID = %s, want %s", got.UserID, want.UserID)
	}
	if got.DriverID != want.DriverID {
		t.Errorf("Trip.DriverID = %s, want %s", got.DriverID, want.DriverID)
	}
	if got.StartLatitude != want.StartLatitude || got.StartLongitude != want.StartLongitude {
		t.Errorf(
			"Trip start coordinates = (%v, %v), want (%v, %v)",
			got.StartLatitude,
			got.StartLongitude,
			want.StartLatitude,
			want.StartLongitude,
		)
	}
	if got.EndLatitude != want.EndLatitude || got.EndLongitude != want.EndLongitude {
		t.Errorf(
			"Trip end coordinates = (%v, %v), want (%v, %v)",
			got.EndLatitude,
			got.EndLongitude,
			want.EndLatitude,
			want.EndLongitude,
		)
	}
	if got.Price != want.Price {
		t.Errorf("Trip.Price = %d, want %d", got.Price, want.Price)
	}
	if got.Status != want.Status {
		t.Errorf("Trip.Status = %q, want %q", got.Status, want.Status)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("Trip.StartedAt = %v, want %v", got.StartedAt, want.StartedAt)
	}
	assertOptionalTime(t, "Trip.FinishedAt", got.FinishedAt, want.FinishedAt)
	if !want.CreatedAt.IsZero() && !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("Trip.CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !want.UpdatedAt.IsZero() && !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("Trip.UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
}

func assertOptionalTime(t *testing.T, field string, got, want *time.Time) {
	t.Helper()

	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("%s = %v, want %v", field, got, want)
		}

		return
	}
	if !got.Equal(*want) {
		t.Errorf("%s = %v, want %v", field, *got, *want)
	}
}
