package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/ogrock3t/go-lw-avito/internal/config"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/handler"
	"github.com/ogrock3t/go-lw-avito/internal/storage/postgres"
	"github.com/ogrock3t/go-lw-avito/internal/trip"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	pool, err := postgres.NewPool(ctx, cfg.Database)
	if err != nil {
		slog.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	tripRepo := postgres.NewTripRepository(pool)
	txManager := postgres.NewTxManager(pool)
	tripService := trip.NewService(tripRepo, txManager)

	apiServer := handler.NewServer(tripService, pool, cfg.Database.QueryTimeout)

	router := chi.NewRouter()
	httpHandler := api.HandlerWithOptions(apiServer, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: handler.GeneratedErrorHandler,
	})

	server := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           httpHandler,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	serverErrCh := make(chan error, 1)

	go func() {
		slog.Info("server started", "http_addr", cfg.HTTP.Addr)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownCh)

	select {
	case err := <-serverErrCh:
		slog.Error("server failed", "error", err)
		os.Exit(1)

	case sig := <-shutdownCh:
		slog.Info("shutdown signal received", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown server", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
