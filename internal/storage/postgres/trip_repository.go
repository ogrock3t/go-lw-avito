package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ogrock3t/go-lw-avito/internal/trip"
)

const uniqueViolationCode = "23505"

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

type TripRepository struct {
	pool *pgxpool.Pool
}

func NewTripRepository(pool *pgxpool.Pool) *TripRepository {
	return &TripRepository{
		pool: pool,
	}
}

func (r *TripRepository) Create(ctx context.Context, t trip.Trip) error {
	query, args, err := psql.
		Insert("trips").
		Columns(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
		).
		Values(
			t.ID,
			t.UserID,
			t.DriverID,
			t.StartLatitude,
			t.StartLongitude,
			t.EndLatitude,
			t.EndLongitude,
			t.Price,
			string(t.Status),
			t.StartedAt,
			t.FinishedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build create trip query: %w", err)
	}

	executor := executorFromContext(ctx, r.pool)

	if _, err := executor.Exec(ctx, query, args...); err != nil {
		if isUniqueViolation(err) {
			return trip.ErrDriverBusy
		}

		return fmt.Errorf("create trip: %w", err)
	}

	return nil
}

func (r *TripRepository) CreateStatusHistory(ctx context.Context, history trip.StatusHistory) error {
	query, args, err := psql.
		Insert("trip_status_history").
		Columns(
			"trip_id",
			"from_status",
			"to_status",
			"reason",
			"changed_at",
		).
		Values(
			history.TripID,
			statusToNullableString(history.FromStatus),
			string(history.ToStatus),
			history.Reason,
			history.ChangedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build create status history query: %w", err)
	}

	executor := executorFromContext(ctx, r.pool)

	if _, err := executor.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("create status history: %w", err)
	}

	return nil
}

func (r *TripRepository) GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	query, args, err := selectTripBuilder().
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	executor := executorFromContext(ctx, r.pool)

	t, err := scanTrip(executor.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Trip{}, trip.ErrTripNotFound
		}

		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return t, nil
}

func (r *TripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (trip.Trip, error) {
	query, args, err := psql.
		Update("trips").
		Set("status", string(trip.StatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.Eq{"id": id}).
		Where(sq.Eq{"status": string(trip.StatusActive)}).
		Suffix(returningTripColumns()).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build finish trip query: %w", err)
	}

	executor := executorFromContext(ctx, r.pool)

	t, err := scanTrip(executor.QueryRow(ctx, query, args...))
	if err == nil {
		return t, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}

	status, err := r.getStatusByID(ctx, id)
	if err != nil {
		return trip.Trip{}, err
	}

	if status == trip.StatusCompleted {
		return trip.Trip{}, trip.ErrTripCompleted
	}

	return trip.Trip{}, fmt.Errorf("unexpected trip status %q", status)
}

func (r *TripRepository) getStatusByID(ctx context.Context, id uuid.UUID) (trip.Status, error) {
	query, args, err := psql.
		Select("status").
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return "", fmt.Errorf("build get trip status query: %w", err)
	}

	executor := executorFromContext(ctx, r.pool)

	var status string
	if err := executor.QueryRow(ctx, query, args...).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", trip.ErrTripNotFound
		}

		return "", fmt.Errorf("get trip status: %w", err)
	}

	return trip.Status(status), nil
}

func selectTripBuilder() sq.SelectBuilder {
	return psql.
		Select(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
			"created_at",
			"updated_at",
		).
		From("trips")
}

func returningTripColumns() string {
	return `RETURNING
		id,
		user_id,
		driver_id,
		start_latitude,
		start_longitude,
		end_latitude,
		end_longitude,
		price,
		status,
		started_at,
		finished_at,
		created_at,
		updated_at`
}

func scanTrip(row pgx.Row) (trip.Trip, error) {
	var t trip.Trip
	var status string
	var finishedAt pgtype.Timestamptz

	err := row.Scan(
		&t.ID,
		&t.UserID,
		&t.DriverID,
		&t.StartLatitude,
		&t.StartLongitude,
		&t.EndLatitude,
		&t.EndLongitude,
		&t.Price,
		&status,
		&t.StartedAt,
		&finishedAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return trip.Trip{}, err
	}

	t.Status = trip.Status(status)

	if finishedAt.Valid {
		t.FinishedAt = &finishedAt.Time
	}

	return t, nil
}

func statusToNullableString(status *trip.Status) any {
	if status == nil {
		return nil
	}

	return string(*status)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == uniqueViolationCode
}
