package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
)

func (s *server) GetTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	ctx, cancel := context.WithTimeout(r.Context(), s.queryTimeout)
	defer cancel()

	t, err := s.service.GetTrip(ctx, uuid.UUID(tripId))
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPITrip(t))
}
