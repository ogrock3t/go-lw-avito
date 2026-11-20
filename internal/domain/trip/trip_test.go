package trip_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
)

func TestTrip_Finish(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	finishAt := startedAt.Add(time.Hour)
	alreadyFinishedAt := startedAt.Add(30 * time.Minute)

	activeTrip := trip.Trip{
		ID:        uuid.New(),
		Status:    trip.StatusActive,
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}
	completedTrip := trip.Trip{
		ID:         uuid.New(),
		Status:     trip.StatusCompleted,
		StartedAt:  startedAt,
		FinishedAt: &alreadyFinishedAt,
		UpdatedAt:  alreadyFinishedAt,
	}

	tests := []struct {
		name    string
		initial trip.Trip
		at      time.Time
		want    trip.Trip
		wantErr error
	}{
		{
			name:    "active trip is completed",
			initial: activeTrip,
			at:      finishAt,
			want: trip.Trip{
				ID:         activeTrip.ID,
				Status:     trip.StatusCompleted,
				StartedAt:  startedAt,
				FinishedAt: &finishAt,
				UpdatedAt:  finishAt,
			},
		},
		{
			name:    "completed trip returns error without changes",
			initial: completedTrip,
			at:      finishAt,
			want:    completedTrip,
			wantErr: trip.ErrTripCompleted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := test.initial
			err := got.Finish(test.at)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("Finish() error = %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("Finish() trip = %+v, want %+v", got, test.want)
			}
		})
	}
}
