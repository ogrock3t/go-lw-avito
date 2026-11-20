package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	nethttp "net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ogrock3t/go-lw-avito/internal/config"
	"github.com/ogrock3t/go-lw-avito/internal/gateway/push"
	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	transporthttp "github.com/ogrock3t/go-lw-avito/internal/http"
	"github.com/ogrock3t/go-lw-avito/internal/http/domainerror"
	"github.com/ogrock3t/go-lw-avito/internal/http/health"
	httpmiddleware "github.com/ogrock3t/go-lw-avito/internal/http/middleware"
	createhttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/create"
	finishhttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/finish"
	gethttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/get"
	getpositionshttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/getpositions"
	savepositionshttp "github.com/ogrock3t/go-lw-avito/internal/http/trip/saveposititons"
	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres"
	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres/transaction"
	telemetryinfra "github.com/ogrock3t/go-lw-avito/internal/infra/telemetry"
	"github.com/ogrock3t/go-lw-avito/internal/repository/trip"
	createuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/create"
	finishuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/finish"
	getuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/get"
	getpositionsuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/getpositions"
	savepositionsuc "github.com/ogrock3t/go-lw-avito/internal/usecase/trip/savepositions"
	"github.com/ogrock3t/go-lw-avito/internal/worker/positions"
)

type Container struct {
	HTTPServer *nethttp.Server
	Logger     *slog.Logger

	pool      *pgxpool.Pool
	telemetry *telemetryinfra.Telemetry

	pushGateway    *push.Gateway
	positionTicker positions.Ticker
	positionWorker *positions.Worker

	cancelHTTP      context.CancelFunc
	httpConnections sync.WaitGroup
}

func New(ctx context.Context, cfg config.Config) (_ *Container, err error) {
	telemetryService, err := telemetryinfra.New(ctx, cfg.Telemetry, cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("initialize telemetry: %w", err)
	}

	app := &Container{
		Logger:    telemetryService.Logger,
		telemetry: telemetryService,
	}

	defer func() {
		if err == nil {
			return
		}

		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			cfg.ShutdownTimeout,
		)
		defer cancel()

		if cleanupErr := app.closeResources(cleanupCtx); cleanupErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf("clean up container: %w", cleanupErr),
			)
		}
	}()

	app.pool, err = postgres.NewPool(
		ctx,
		cfg.Database,
		telemetryService.TracerProvider,
		telemetryService.MeterProvider,
	)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	tripMetrics, err := telemetryinfra.NewTripMetrics(
		telemetryService.MeterProvider,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize trip metrics: %w", err)
	}

	tripRepository := trip.NewRepository(app.pool)
	txManager := transaction.NewTxManager(app.pool)

	positionQueue, err := positions.NewQueue(cfg.Positions.QueueSize)
	if err != nil {
		return nil, fmt.Errorf("create position queue: %w", err)
	}

	positionMetrics, err := positions.NewMetrics(
		telemetryService.MeterProvider,
		positionQueue,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize position metrics: %w", err)
	}

	app.pushGateway, err = push.NewGateway(
		cfg.Push.HTTPURL,
		cfg.Push.RequestTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("create push gateway: %w", err)
	}

	app.positionTicker, err = positions.NewTicker(
		cfg.Positions.CheckInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("create position ticker: %w", err)
	}

	positionTracer := telemetryService.TracerProvider.Tracer(
		"github.com/ogrock3t/go-lw-avito/internal/worker/positions",
	)

	app.positionWorker, err = positions.NewWorker(
		positionQueue,
		cfg.Positions.Workers,
		cfg.Push.RequestTimeout,
		app.pushGateway,
		telemetryService.Logger,
		tripRepository,
		app.positionTicker,
		cfg.Positions.StaleAfter,
		positionMetrics,
		positionTracer,
	)
	if err != nil {
		return nil, fmt.Errorf("create position worker: %w", err)
	}

	createUseCase := createuc.NewUseCase(
		tripRepository,
		txManager,
		telemetryService.Logger,
		tripMetrics,
	)
	getUseCase := getuc.NewUseCase(tripRepository)
	finishUseCase := finishuc.NewUseCase(
		tripRepository,
		txManager,
		telemetryService.Logger,
		tripMetrics,
	)
	savePositionUseCase := savepositionsuc.NewUseCase(
		tripRepository,
		txManager,
		telemetryService.Logger,
		tripMetrics,
		positionQueue,
	)
	getPositionsUseCase := getpositionsuc.NewUseCase(tripRepository)

	createHandler := createhttp.NewHandler(
		createUseCase,
		cfg.Database.QueryTimeout,
		telemetryService.Logger,
	)
	getHandler := gethttp.NewHandler(
		getUseCase,
		cfg.Database.QueryTimeout,
		telemetryService.Logger,
	)
	finishHandler := finishhttp.NewHandler(
		finishUseCase,
		cfg.Database.QueryTimeout,
		telemetryService.Logger,
	)
	healthHandler := health.NewHandler(
		app.pool,
		cfg.Database.QueryTimeout,
		telemetryService.Logger,
	)
	savePositionHandler := savepositionshttp.NewHandler(
		savePositionUseCase,
		cfg.Database.QueryTimeout,
		telemetryService.Logger,
	)
	getPositionsHandler := getpositionshttp.NewHandler(
		getPositionsUseCase,
		cfg.Database.QueryTimeout,
		telemetryService.Logger,
	)

	apiServer := transporthttp.NewServer(
		createHandler,
		getHandler,
		finishHandler,
		healthHandler,
		savePositionHandler,
		getPositionsHandler,
	)

	httpMetrics, err := httpmiddleware.NewHTTPMetrics(
		telemetryService.MeterProvider,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize HTTP metrics: %w", err)
	}

	router := chi.NewRouter()
	router.Use(
		httpmiddleware.Tracing(telemetryService.TracerProvider),
		httpmiddleware.Logging(telemetryService.Logger),
		httpMetrics.Middleware(router),
	)

	handler := api.HandlerWithOptions(apiServer, api.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: domainerror.NewGeneratedErrorHandler(
			telemetryService.Logger,
		),
	})

	handler = httpmiddleware.Recover(telemetryService.Logger)(handler)

	httpCtx, cancelHTTP := context.WithCancel(ctx)
	app.cancelHTTP = cancelHTTP

	app.HTTPServer = &nethttp.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           handler,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		BaseContext: func(_ net.Listener) context.Context {
			return httpCtx
		},
		ConnState: func(_ net.Conn, state nethttp.ConnState) {
			switch state {
			case nethttp.StateNew:
				app.httpConnections.Add(1)

			case nethttp.StateClosed, nethttp.StateHijacked:
				app.httpConnections.Done()
			}
		},
	}

	return app, nil
}

func (c *Container) Start(ctx context.Context) error {
	if err := c.positionWorker.Start(ctx); err != nil {
		return fmt.Errorf("start position worker: %w", err)
	}

	return nil
}

func (c *Container) Close(ctx context.Context) error {
	var shutdownErr error

	if err := c.HTTPServer.Shutdown(ctx); err != nil {
		shutdownErr = fmt.Errorf("shutdown HTTP server: %w", err)

		c.cancelHTTP()

		if closeErr := c.HTTPServer.Close(); closeErr != nil {
			shutdownErr = errors.Join(
				shutdownErr,
				fmt.Errorf("close HTTP server: %w", closeErr),
			)
		}
	}

	c.cancelHTTP()
	c.httpConnections.Wait()

	if err := c.positionWorker.Shutdown(ctx); err != nil {
		shutdownErr = errors.Join(
			shutdownErr,
			fmt.Errorf("shutdown position worker: %w", err),
		)
	}

	if err := c.closeResources(ctx); err != nil {
		c.Logger.Warn(
			"failed to shut down telemetry",
			slog.Any("error", err),
		)
	}

	return shutdownErr
}

func (c *Container) closeResources(ctx context.Context) error {
	if c.cancelHTTP != nil {
		c.cancelHTTP()
	}

	if c.positionTicker != nil {
		c.positionTicker.Stop()
	}

	if c.pushGateway != nil {
		c.pushGateway.CloseIdleConnections()
	}

	if c.pool != nil {
		c.pool.Close()
	}

	if err := c.telemetry.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown telemetry: %w", err)
	}

	return nil
}
