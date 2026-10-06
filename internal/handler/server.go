package handler

import (
	"time"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
)

type server struct {
	api.Unimplemented

	service      tripService
	pool         databasePool
	queryTimeout time.Duration
}

func NewServer(service tripService, pool databasePool, queryTimeout time.Duration) *server {
	return &server{
		service:      service,
		pool:         pool,
		queryTimeout: queryTimeout,
	}
}