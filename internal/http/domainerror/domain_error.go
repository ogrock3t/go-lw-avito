package domainerror

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ogrock3t/go-lw-avito/internal/domain/trip"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
)

func Problem(err error) (int, api.Problem) {
	switch {
	case errors.Is(err, trip.ErrTripNotFound):
		return newProblem(http.StatusNotFound, "trip_not_found", "Trip not found", "Trip not found")
	case errors.Is(err, trip.ErrDriverBusy):
		return newProblem(http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrTripCompleted):
		return newProblem(http.StatusConflict, "trip_completed", "Trip completed", "Trip already completed")
	default:
		return newProblem(http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
	}
}

func newProblem(status int32, code, title, detail string) (int, api.Problem) {
	return int(status), api.Problem{
		Type:   "https://tripgo.example/problems/" + code,
		Title:  title,
		Status: status,
		Detail: &detail,
		Code:   code,
	}
}

func NewGeneratedErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	writer := response.NewWriter(logger)
	return func(w http.ResponseWriter, r *http.Request, err error) {
		detail := "Invalid path or header parameter"

		var invalid *api.InvalidParamFormatError
		var multiple *api.TooManyValuesForParamError

		switch {
		case errors.As(err, &invalid):
			detail = invalid.ParamName + " must be a valid UUID"
		case errors.As(err, &multiple):
			detail = multiple.ParamName + " must have exactly one value"
		}

		writer.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
	}
}
