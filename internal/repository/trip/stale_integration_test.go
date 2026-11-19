//go:build integration

package trip_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/model"
)

func TestRepository_ListStaleTrip(t *testing.T) {
	ctx, pool, repository := newTestRepository(t)

	var now time.Time
	if err := pool.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		t.Fatalf("read database time: %v", err)
	}

	staleAt := now.Add(-10 * time.Minute)
	freshAt := now.Add(-time.Minute)

	fixtures := []struct {
		name           string
		tripID         uuid.UUID
		driverID       uuid.UUID
		startedAgo     time.Duration
		lastPositionAt *time.Time
		completed      bool
	}{
		{
			name:           "stale position",
			startedAgo:     time.Hour,
			lastPositionAt: &staleAt,
		},
		{
			name:       "stale without positions",
			startedAgo: 20 * time.Minute,
		},
		{
			name:           "fresh position",
			startedAgo:     time.Hour,
			lastPositionAt: &freshAt,
		},
		{
			name:       "fresh without positions",
			startedAgo: time.Minute,
		},
		{
			name:       "completed",
			startedAgo: time.Hour,
			completed:  true,
		},
	}

	fixtureIDs := make(map[uuid.UUID]bool)

	for i := range fixtures {
		fixture := &fixtures[i]
		createdTrip := newActiveTrip()
		createdTrip.StartedAt = now.Add(-fixture.startedAgo)

		if fixture.completed {
			if err := createdTrip.Finish(now); err != nil {
				t.Fatalf("%s: finish trip: %v", fixture.name, err)
			}
		}

		fixture.tripID = createdTrip.ID
		fixture.driverID = createdTrip.DriverID
		fixtureIDs[createdTrip.ID] = true

		cleanupTrips(t, pool, createdTrip.ID)
		insertTrip(t, ctx, pool, createdTrip)

		if fixture.lastPositionAt != nil {
			_, err := pool.Exec(
				ctx,
				"UPDATE trips SET last_position_at = $1 WHERE id = $2",
				*fixture.lastPositionAt,
				createdTrip.ID,
			)
			if err != nil {
				t.Fatalf("%s: set last_position_at: %v", fixture.name, err)
			}
		}
	}

	result, err := repository.ListStaleTrip(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("ListStaleTrip() error: %v", err)
	}

	got := make([]model.StaleTrip, 0)
	for _, candidate := range result {
		if fixtureIDs[candidate.TripID] {
			got = append(got, candidate)
		}
	}

	want := []model.StaleTrip{
		{
			TripID:   fixtures[1].tripID,
			DriverID: fixtures[1].driverID,
		},
		{
			TripID:   fixtures[0].tripID,
			DriverID: fixtures[0].driverID,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListStaleTrip() = %+v, want %+v", got, want)
	}
}
