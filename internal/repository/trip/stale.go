package trip

import (
	"context"
	"fmt"
	"github.com/ogrock3t/go-lw-avito/internal/model"
	"time"
)

func (r *Repository) ListStaleTrip(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error) {
	query, args, err := psql.
		Select("id", "driver_id", "COALESCE(last_position_at, started_at)").
		From("trips").
		Where("status = 'active'").
		Where(
			"COALESCE(last_position_at, started_at) < now() - make_interval(secs => ?::double precision)",
			staleAfter.Seconds(),
		).
		OrderBy("COALESCE(last_position_at, started_at) ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list stale trips query: %w", err)
	}

	rows, err := r.executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query stale trips: %w", err)
	}
	defer rows.Close()

	var trips []model.StaleTrip

	for rows.Next() {
		var trip model.StaleTrip

		if err := rows.Scan(&trip.TripID, &trip.DriverID, &trip.LastActivityAt); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}

		trips = append(trips, trip)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}

	return trips, nil
}
