package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ogrock3t/go-lw-avito/internal/config"
	"github.com/ogrock3t/go-lw-avito/internal/container"
)

func main() {
	if err := run(); err != nil {
		bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

		bootstrapLogger.Error("service stopped with error", slog.Any("error", err))

		os.Exit(1)
	}
}

func run() (runErr error) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	app, err := container.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	logger := app.Logger

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := app.Close(closeCtx); err != nil {
			logger.Warn("failed to shut down telemetry", slog.Any("error", err))
		}
	}()

	serverErrCh := make(chan error, 1)

	go func() {
		logger.Info("server started", slog.String("http_addr", cfg.HTTP.Addr))

		if err := app.HTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownCh)

	select {
	case err := <-serverErrCh:
		return err

	case sig := <-shutdownCh:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := app.HTTPServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	logger.Info("server stopped")

	return nil
}
