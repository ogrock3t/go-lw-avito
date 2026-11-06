package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	"github.com/ogrock3t/go-lw-avito/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		err               error
		wantStatus        int
		wantProblemStatus int32
		wantCode          string
		wantTitle         string
		wantDetail        string
	}{
		{
			name:              "driver busy",
			err:               fmt.Errorf("create trip: %w", trip.ErrDriverBusy),
			wantStatus:        http.StatusConflict,
			wantProblemStatus: http.StatusConflict,
			wantCode:          "driver_busy",
			wantTitle:         "Driver busy",
			wantDetail:        "Driver already has an active trip",
		},
		{
			name:              "trip completed",
			err:               fmt.Errorf("finish trip: %w", trip.ErrTripCompleted),
			wantStatus:        http.StatusConflict,
			wantProblemStatus: http.StatusConflict,
			wantCode:          "trip_completed",
			wantTitle:         "Trip completed",
			wantDetail:        "Trip already completed",
		},
		{
			name:              "trip not found",
			err:               fmt.Errorf("get trip: %w", trip.ErrTripNotFound),
			wantStatus:        http.StatusNotFound,
			wantProblemStatus: http.StatusNotFound,
			wantCode:          "trip_not_found",
			wantTitle:         "Trip not found",
			wantDetail:        "Trip not found",
		},
		{
			name:              "unknown error",
			err:               errors.New("database connection failed: password=secret"),
			wantStatus:        http.StatusInternalServerError,
			wantProblemStatus: http.StatusInternalServerError,
			wantCode:          "internal_error",
			wantTitle:         "Internal error",
			wantDetail:        "Internal server error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, problem := domainerror.Problem(test.err)

			if status != test.wantStatus {
				t.Errorf("Problem() status = %d, want %d", status, test.wantStatus)
			}
			if problem.Status != test.wantProblemStatus {
				t.Errorf("Problem() body status = %d, want %d", problem.Status, test.wantProblemStatus)
			}
			if problem.Code != test.wantCode {
				t.Errorf("Problem() code = %q, want %q", problem.Code, test.wantCode)
			}
			if problem.Title != test.wantTitle {
				t.Errorf("Problem() title = %q, want %q", problem.Title, test.wantTitle)
			}
			if problem.Detail == nil || *problem.Detail != test.wantDetail {
				t.Errorf("Problem() detail = %v, want %q", problem.Detail, test.wantDetail)
			}
		})
	}
}
