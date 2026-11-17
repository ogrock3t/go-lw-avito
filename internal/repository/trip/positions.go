package trip

import (
	"context"
	"fmt"
	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"time"
)

func (r *Repository) InsertPositions(ctx context.Context, position trip.Position) (trip.Position, error) {
	query, args, err := psql.
		Insert("trip_positions").
		Columns(
			"trip_id",
			"latitude",
			"longitude",
			"recorded_at",
		).
		Values(
			position.TripId,
			position.Latitude,
			position.Longitude,
			position.RecordedAt,
		).
		Suffix("RETURNING id, trip_id, latitude, longitude, recorded_at, created_at").
		ToSql()
	if err != nil {
		return trip.Position{}, fmt.Errorf("build insert positions query: %w", err)
	}

	executor := r.executor(ctx)

	saved, err := scanPosition(executor.QueryRow(ctx, query, args...))
	if err != nil {
		return trip.Position{}, fmt.Errorf("insert trip positions: %w", err)
	}

	return saved, nil
}

func (r *Repository) UpdateLastPosition(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error {
	query, args, err := psql.
		Update("trips").
		Set("last_position_at", sq.Expr("GREATEST(last_position_at, ?)", recordedAt)).
		Where(sq.Eq{"id": tripID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update last position query: %w", err)
	}

	executor := r.executor(ctx)

	result, err := executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update last position query: %w", err)
	}

	if result.RowsAffected() == 0 {
		return trip.ErrTripNotFound
	}

	return nil
}

func (r *Repository) ListPositions(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error) {
	query, args, err := psql.
		Select("id", "trip_id", "latitude", "longitude", "recorded_at", "created_at").
		From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list trip positions query: %w", err)
	}

	rows, err := r.executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trip positions: %w", err)
	}
	defer rows.Close()

	positions := make([]trip.Position, 0)
	for rows.Next() {
		position, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan trip positions: %w", err)
		}

		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read trip positions: %w", err)
	}

	return positions, nil
}
