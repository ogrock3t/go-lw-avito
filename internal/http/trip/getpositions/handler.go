package getpositions

import (
	"context"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/domainerror"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip"
	"log/slog"
	"net/http"
	"time"
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

func (h *Handler) ListTripPositions(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	positions, err := h.useCase.ListTripPositions(ctx, tripID)
	if err != nil {
		status, problem := domainerror.Problem(err)
		if status == http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), "list trip positions", slog.Any("error", err))
		}

		instance := r.URL.Path
		problem.Instance = &instance
		h.response.WriteProblemBody(w, r, status, problem)
		return
	}

	body := api.TripPositionsResponse{
		Positions: make([]api.TripPosition, 0, len(positions)),
	}
	for _, position := range positions {
		body.Positions = append(body.Positions, trip.ToAPIPosition(position))
	}

	h.response.WriteJSON(w, r, http.StatusOK, body)
}
