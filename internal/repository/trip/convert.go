package trip

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

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
