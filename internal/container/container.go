package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ogrock3t/go-lw-avito/internal/config"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	transporthttp "github.com/ogrock3t/go-lw-avito/internal/http"
	"github.com/ogrock3t/go-lw-avito/internal/http/domainerror"
	"github.com/ogrock3t/go-lw-avito/internal/http/health"
	httpmiddleware "github.com/ogrock3t/go-lw-avito/internal/http/middleware"
	createhttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/create"
	finishhttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/finish"
	gethttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/get"
	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres"
	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres/transaction"
	telemetryinfra "github.com/ogrock3t/go-lw-avito/internal/infra/telemetry"
	"github.com/ogrock3t/go-lw-avito/internal/repository/trip"
	createuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/create"
	finishuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/finish"
	getuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/get"
)

type Container struct {
	HTTPServer *nethttp.Server
	Logger     *slog.Logger

	pool      *pgxpool.Pool
	telemetry *telemetryinfra.Telemetry
}

func New(ctx context.Context, cfg config.Config) (*Container, error) {
	telemetryService, err := telemetryinfra.New(ctx, cfg.Telemetry, cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("initialize telemetry: %w", err)
	}

	pool, err := postgres.NewPool(ctx, cfg.Database, telemetryService.TracerProvider, telemetryService.MeterProvider)
	if err != nil {
		shutdownErr := telemetryService.Shutdown(ctx)

		return nil, errors.Join(fmt.Errorf("create postgres pool: %w", err), shutdownErr)
	}

	tripMetrics, err := telemetryinfra.NewTripMetrics(telemetryService.MeterProvider)
	if err != nil {
		pool.Close()
		shutdownErr := telemetryService.Shutdown(ctx)

		return nil, errors.Join(fmt.Errorf("initialize trip metrics: %w", err), shutdownErr)
	}

	tripRepository := trip.NewRepository(pool)
	txManager := transaction.NewTxManager(pool)

	createUseCase := createuc.NewUseCase(tripRepository, txManager, telemetryService.Logger, tripMetrics)
	getUseCase := getuc.NewUseCase(tripRepository)
	finishUseCase := finishuc.NewUseCase(tripRepository, txManager, telemetryService.Logger, tripMetrics)

	createHandler := createhttp.NewHandler(createUseCase, cfg.Database.QueryTimeout, telemetryService.Logger)
	getHandler := gethttp.NewHandler(getUseCase, cfg.Database.QueryTimeout, telemetryService.Logger)
	finishHandler := finishhttp.NewHandler(finishUseCase, cfg.Database.QueryTimeout, telemetryService.Logger)
	healthHandler := health.NewHandler(pool, cfg.Database.QueryTimeout, telemetryService.Logger)

	apiServer := transporthttp.NewServer(
		createHandler,
		getHandler,
		finishHandler,
		healthHandler,
	)

	httpMetrics, err := httpmiddleware.NewHTTPMetrics(telemetryService.MeterProvider)
	if err != nil {
		pool.Close()
		shutdownErr := telemetryService.Shutdown(ctx)

		return nil, errors.Join(fmt.Errorf("initialize HTTP metrics: %w", err), shutdownErr)
	}

	router := chi.NewRouter()

	router.Use(
		httpmiddleware.Tracing(telemetryService.TracerProvider),
		httpmiddleware.Logging(telemetryService.Logger),
		httpMetrics.Middleware(router),
	)

	handler := api.HandlerWithOptions(apiServer, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: domainerror.NewGeneratedErrorHandler(telemetryService.Logger),
	})

	handler = httpmiddleware.Recover(telemetryService.Logger)(handler)

	server := &nethttp.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           handler,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &Container{
		HTTPServer: server,
		Logger:     telemetryService.Logger,
		pool:       pool,
		telemetry:  telemetryService,
	}, nil
}

func (c *Container) Close(ctx context.Context) error {
	c.pool.Close()

	if err := c.telemetry.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown telemetry: %w", err)
	}

	return nil
}
