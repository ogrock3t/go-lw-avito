package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	query, args, err := selectTripBuilder().
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	executor := r.executor(ctx)

	t, err := scanTrip(executor.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Trip{}, trip.ErrTripNotFound
		}

		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return t, nil
}

func (r *Repository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	query, args, err := selectTripBuilder().
		Where(sq.Eq{"id": id}).
		Suffix("FOR UPDATE").
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	executor := r.executor(ctx)

	t, err := scanTrip(executor.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Trip{}, trip.ErrTripNotFound
		}

		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return t, nil
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
