package create

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/domainerror"
	"github.com/ogrock3t/go-lw-avito/internal/http/request"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip"
	"github.com/ogrock3t/go-lw-avito/internal/model"
)

type Handler struct {
	useCase      useCase
	queryTimeout time.Duration
	logger       *slog.Logger
	response     *response.Writer
}

func NewHandler(useCase useCase, queryTimeout time.Duration, logger *slog.Logger) *Handler {
	return &Handler{
		useCase:      useCase,
		queryTimeout: queryTimeout,
		logger:       logger,
		response:     response.NewWriter(logger),
	}
}

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	var body createTripRequest

	if err := request.DecodeJSON(r, &body); err != nil {
		detail := "Invalid JSON body: " + err.Error()
		h.response.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
		return
	}

	if err := validateCreateTripRequest(body); err != nil {
		h.response.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	created, err := h.useCase.CreateTrip(ctx, model.CreateTripInput{
		UserID:         *body.UserID,
		DriverID:       *body.DriverID,
		StartLatitude:  *body.StartPoint.Latitude,
		StartLongitude: *body.StartPoint.Longitude,
		EndLatitude:    *body.EndPoint.Latitude,
		EndLongitude:   *body.EndPoint.Longitude,
		Price:          *body.Price,
	})
	if err != nil {
		status, problem := domainerror.Problem(err)
		if status == http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), "create trip", slog.Any("error", err))
		}

		instance := r.URL.Path
		problem.Instance = &instance
		h.response.WriteProblemBody(w, r, status, problem)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+created.ID.String())
	h.response.WriteJSON(w, r, http.StatusCreated, trip.ToAPITrip(created))
}
