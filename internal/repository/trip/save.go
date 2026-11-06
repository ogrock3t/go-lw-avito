package trip

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

func (r *Repository) Create(ctx context.Context, t trip.Trip) error {
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

	executor := r.executor(ctx)

	if _, err := executor.Exec(ctx, query, args...); err != nil {
		if isDriverBusyViolation(err) {
			return trip.ErrDriverBusy
		}

		return fmt.Errorf("create trip: %w", err)
	}

	return nil
}

func (r *Repository) CreateStatusHistory(ctx context.Context, history trip.StatusHistory) error {
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

	executor := r.executor(ctx)

	if _, err := executor.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("create status history: %w", err)
	}

	return nil
}

func (r *Repository) Update(ctx context.Context, t trip.Trip) error {
	query, args, err := psql.
		Update("trips").
		Set("status", string(t.Status)).
		Set("finished_at", t.FinishedAt).
		Set("updated_at", t.UpdatedAt).
		Where(sq.Eq{"id": t.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update trip query: %w", err)
	}

	executor := r.executor(ctx)

	result, err := executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update trip: %w", err)
	}

	if result.RowsAffected() == 0 {
		return trip.ErrTripNotFound
	}

	return nil
}
