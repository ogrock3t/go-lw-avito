package get_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/usecase/trip/get"
)

func TestUseCase_GetTrip_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)

	tripID := uuid.New()

	repo.EXPECT().
		GetByID(gomock.Any(), tripID).
		Return(trip.Trip{}, trip.ErrTripNotFound)

	useCase := get.NewUseCase(repo)

	got, err := useCase.GetTrip(context.Background(), tripID)
	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("GetTrip() error = %v, want %v", err, trip.ErrTripNotFound)
	}

	if !reflect.DeepEqual(got, trip.Trip{}) {
		t.Errorf("GetTrip() trip = %+v, want zero value", got)
	}
}
