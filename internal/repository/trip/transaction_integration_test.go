//go:build integration

package trip_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domaintrip "github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres/transaction"
	triprepo "github.com/ogrock3t/go-lw-avito/internal/repository/trip"
)

func TestCreateTrip_RollsBackWhenStatusHistoryInsertFails(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	tripID := uuid.New()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM trip_status_history WHERE trip_id = $1", tripID); err != nil {
			t.Errorf("cleanup trip status history: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM trips WHERE id = $1", tripID); err != nil {
			t.Errorf("cleanup trip: %v", err)
		}
	})

	repository := triprepo.NewRepository(pool)
	txManager := transaction.NewTxManager(pool)
	now := time.Now().UTC()

	createdTrip := domaintrip.Trip{
		ID:             tripID,
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

	var historyInsertErr error

	err = txManager.Do(ctx, func(txCtx context.Context) error {
		if err := repository.Create(txCtx, createdTrip); err != nil {
			return fmt.Errorf("create trip: %w", err)
		}

		var count int
		executor := transaction.ExecutorFromContext(txCtx, pool)
		if err := executor.QueryRow(txCtx, "SELECT count(*) FROM trips WHERE id = $1", tripID).Scan(&count); err != nil {
			return fmt.Errorf("check trip inside transaction: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("trip count inside transaction = %d, want 1", count)
		}

		historyInsertErr = repository.CreateStatusHistory(txCtx, domaintrip.StatusHistory{
			TripID:    tripID,
			ToStatus:  domaintrip.Status("invalid"),
			Reason:    "force rollback",
			ChangedAt: now,
		})

		return historyInsertErr
	})
	if historyInsertErr == nil {
		t.Fatal("CreateStatusHistory() error = nil, want database constraint error")
	}
	if err == nil {
		t.Fatal("transaction error = nil, want rollback-triggering error")
	}

	assertRowCount(t, ctx, pool, "trips", "SELECT count(*) FROM trips WHERE id = $1", tripID, 0)
	assertRowCount(
		t,
		ctx,
		pool,
		"trip_status_history",
		"SELECT count(*) FROM trip_status_history WHERE trip_id = $1",
		tripID,
		0,
	)
}

func assertRowCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	query string,
	tripID uuid.UUID,
	want int,
) {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, query, tripID).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	if count != want {
		t.Errorf("rows in %s = %d, want %d", table, count, want)
	}
}
