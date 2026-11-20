package getpositions_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/usecase/trip/getpositions"
)

func TestUseCase_ListTripPositions_ReturnsRoute(t *testing.T) {
	t.Parallel()
	tripID := uuid.New()
	positions := []trip.Position{
		{ID: 1, TripId: tripID, Latitude: 55.75, Longitude: 37.61, RecordedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)},
		{ID: 2, TripId: tripID, Latitude: 55.76, Longitude: 37.62, RecordedAt: time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)},
	}
	tests := []struct {
		name      string
		status    trip.Status
		positions []trip.Position
	}{
		{name: "active trip", status: trip.StatusActive, positions: positions},
		{name: "completed trip", status: trip.StatusCompleted, positions: positions},
		{name: "empty route", status: trip.StatusActive, positions: []trip.Position{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := NewMocktripRepository(gomock.NewController(t))
			ctx := context.Background()
			gomock.InOrder(
				repo.EXPECT().GetByID(ctx, tripID).Return(trip.Trip{ID: tripID, Status: test.status}, nil),
				repo.EXPECT().ListPositions(ctx, tripID).Return(test.positions, nil),
			)

			got, err := getpositions.NewUseCase(repo).ListTripPositions(ctx, tripID)
			if err != nil {
				t.Fatalf("ListTripPositions() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.positions) {
				t.Errorf("ListTripPositions() = %+v, want %+v", got, test.positions)
			}
		})
	}
}

func TestUseCase_ListTripPositions_TripLookupErrors(t *testing.T) {
	t.Parallel()
	for _, lookupErr := range []error{trip.ErrTripNotFound, errors.New("database unavailable")} {
		t.Run(lookupErr.Error(), func(t *testing.T) {
			t.Parallel()
			repo := NewMocktripRepository(gomock.NewController(t))
			tripID := uuid.New()
			repo.EXPECT().GetByID(gomock.Any(), tripID).Return(trip.Trip{}, lookupErr)

			got, err := getpositions.NewUseCase(repo).ListTripPositions(context.Background(), tripID)
			if !errors.Is(err, lookupErr) {
				t.Fatalf("ListTripPositions() error = %v, want %v", err, lookupErr)
			}
			if got != nil {
				t.Errorf("ListTripPositions() = %+v, want nil on lookup failure", got)
			}
		})
	}
}

func TestUseCase_ListTripPositions_RouteLookupError(t *testing.T) {
	t.Parallel()
	repo := NewMocktripRepository(gomock.NewController(t))
	tripID := uuid.New()
	routeErr := errors.New("positions query failed")
	gomock.InOrder(
		repo.EXPECT().GetByID(gomock.Any(), tripID).Return(trip.Trip{ID: tripID}, nil),
		repo.EXPECT().ListPositions(gomock.Any(), tripID).Return(nil, routeErr),
	)

	got, err := getpositions.NewUseCase(repo).ListTripPositions(context.Background(), tripID)
	if !errors.Is(err, routeErr) {
		t.Fatalf("ListTripPositions() error = %v, want %v", err, routeErr)
	}
	if got != nil {
		t.Errorf("ListTripPositions() = %+v, want nil on route lookup failure", got)
	}
}
