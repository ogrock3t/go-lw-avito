package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
)

type Handler struct {
	api.Unimplemented

	pool         databasePool
	queryTimeout time.Duration
	response     *response.Writer
}

func NewHandler(pool databasePool, queryTimeout time.Duration, logger *slog.Logger) *Handler {
	return &Handler{
		pool:         pool,
		queryTimeout: queryTimeout,
		response:     response.NewWriter(logger),
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.response.WriteJSON(w, r, http.StatusOK, api.HealthResponse{
		Status: api.Ok,
	})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		h.response.WriteJSON(w, r, http.StatusServiceUnavailable, api.HealthResponse{
			Status: api.Unavailable,
		})
		return
	}

	h.response.WriteJSON(w, r, http.StatusOK, api.HealthResponse{
		Status: api.Ok,
	})
}
