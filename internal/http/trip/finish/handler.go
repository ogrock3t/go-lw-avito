package finish

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/domainerror"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip"
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

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	t, err := h.useCase.FinishTrip(ctx, tripId)
	if err != nil {
		status, problem := domainerror.Problem(err)
		if status == http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), "finish trip", slog.Any("error", err))
		}

		instance := r.URL.Path
		problem.Instance = &instance
		h.response.WriteProblemBody(w, r, status, problem)
		return
	}

	h.response.WriteJSON(w, r, http.StatusOK, trip.ToAPITrip(t))
}
