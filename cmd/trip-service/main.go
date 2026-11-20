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

	var serverDone chan struct{}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			cfg.ShutdownTimeout,
		)
		defer cancel()

		if err := app.Close(shutdownCtx); err != nil {
			runErr = errors.Join(
				runErr,
				fmt.Errorf("shutdown application: %w", err),
			)
		}

		if serverDone != nil {
			<-serverDone
		}

		logger.Info("service stopped")
	}()

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownCh)

	if err := app.Start(ctx); err != nil {
		return err
	}

	serverErrCh := make(chan error, 1)
	serverDone = make(chan struct{})

	go func() {
		defer close(serverDone)

		logger.Info(
			"starting HTTP server",
			slog.String("http_addr", cfg.HTTP.Addr),
		)

		serverErrCh <- app.HTTPServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}

		return nil

	case sig := <-shutdownCh:
		logger.Info(
			"shutdown signal received",
			slog.String("signal", sig.String()),
		)

		return nil
	}
}
