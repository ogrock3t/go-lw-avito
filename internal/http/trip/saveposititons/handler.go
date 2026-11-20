package saveposititons

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

func (h *Handler) CreateTripPosition(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	var body savePositionRequest

	if err := request.DecodeJSON(r, &body); err != nil {
		detail := "Invalid JSON body: " + err.Error()
		h.response.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
		return
	}

	if err := validateSavePositionRequest(body, time.Now()); err != nil {
		h.response.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	saved, err := h.useCase.SavePosition(ctx, tripID, model.SavePositionsInput{
		Latitude:   *body.Coordinates.Latitude,
		Longitude:  *body.Coordinates.Longitude,
		RecordedAt: body.RecordedAt.UTC(),
	})
	if err != nil {
		status, problem := domainerror.Problem(err)
		if status == http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), "save trip position", slog.Any("error", err))
		}

		instance := r.URL.Path
		problem.Instance = &instance
		h.response.WriteProblemBody(w, r, status, problem)
		return
	}

	h.response.WriteJSON(w, r, http.StatusCreated, trip.ToAPIPosition(saved))
}
