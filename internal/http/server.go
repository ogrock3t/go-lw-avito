package http

import (
	"net/http"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/health"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/create"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/finish"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/get"
	"github.com/ogrock3t/go-lw-avito/internal/http/trip/getpositions"
	savepositionshttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/saveposititons"
)

type Server struct {
	api.Unimplemented

	createHandler       *create.Handler
	getHandler          *get.Handler
	finishHandler       *finish.Handler
	healthHandler       *health.Handler
	savePositionHandler *savepositionshttp.Handler
	getPositionsHandler *getpositions.Handler
}

func NewServer(
	createHandler *create.Handler,
	getHandler *get.Handler,
	finishHandler *finish.Handler,
	healthHandler *health.Handler,
	savePositionHandler *savepositionshttp.Handler,
	getPositionsHandler *getpositions.Handler,
) *Server {
	return &Server{
		createHandler:       createHandler,
		getHandler:          getHandler,
		finishHandler:       finishHandler,
		healthHandler:       healthHandler,
		savePositionHandler: savePositionHandler,
		getPositionsHandler: getPositionsHandler,
	}
}

func (s *Server) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	s.createHandler.CreateTrip(w, r, params)
}

func (s *Server) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.getHandler.GetTrip(w, r, tripID)
}

func (s *Server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.finishHandler.FinishTrip(w, r, tripID)
}

func (s *Server) CreateTripPosition(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.savePositionHandler.CreateTripPosition(w, r, tripID)
}

func (s *Server) ListTripPositions(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.getPositionsHandler.ListTripPositions(w, r, tripID)
}

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	s.healthHandler.Health(w, r)
}

func (s *Server) Ready(w http.ResponseWriter, r *http.Request) {
	s.healthHandler.Ready(w, r)
}
