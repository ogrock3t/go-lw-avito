//go:generate go tool mockgen -source=../worker/positions/contract.go -destination=worker_mocks_test.go -package=container
//go:generate go tool mockgen -destination=telemetry_mocks_test.go -package=container go.opentelemetry.io/otel/sdk/trace SpanProcessor

package container

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/gateway/push"
	"github.com/ogrock3t/go-lw-avito/internal/infra/telemetry"
	"github.com/ogrock3t/go-lw-avito/internal/model"
	"github.com/ogrock3t/go-lw-avito/internal/worker/positions"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestContainer_CloseDrainsHTTPThenWorkersBeforeClosingResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pool, connectErr := newShutdownPool(t, ctx)

	httpFinished := make(chan struct{})
	ticker := NewMockTicker(ctrl)
	ticks := make(chan time.Time)
	scannerStopped := make(chan struct{})
	ticker.EXPECT().C().Return((<-chan time.Time)(ticks))
	gomock.InOrder(
		ticker.EXPECT().Stop().Do(func() {
			select {
			case <-httpFinished:
			default:
				t.Error("scanner stopped before the HTTP request finished")
			}
			close(scannerStopped)
		}),
		ticker.EXPECT().Stop(),
	)
	running := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New(), LastActivityAt: time.Now().Add(-time.Hour)}
	pending := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New(), LastActivityAt: time.Now().Add(-time.Minute)}
	pushStarted := make(chan struct{})
	releasePush := make(chan struct{})
	pushFinished := make(chan uuid.UUID, 2)
	pushConnectionClosed := make(chan struct{})
	var connectionClosed sync.Once
	pushServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data map[string]string `json:"data"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
			t.Errorf("decode push request: %v", decodeErr)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tripID := body.Data["trip_id"]
		if tripID == running.TripID.String() {
			close(pushStarted)
			select {
			case <-releasePush:
			case <-r.Context().Done():
				t.Error("active push canceled before the shutdown deadline")
				return
			}
		} else if tripID != pending.TripID.String() {
			t.Errorf("unexpected push trip = %s", tripID)
		}

		select {
		case <-scannerStopped:
		default:
			t.Error("push completed before the scanner stopped")
		}
		connection, acquireErr := pool.Acquire(r.Context())
		if connection != nil {
			connection.Release()
		}
		if !errors.Is(acquireErr, connectErr) {
			t.Errorf("database pool unavailable while draining workers: %v", acquireErr)
		}
		w.WriteHeader(http.StatusAccepted)
		pushFinished <- uuid.MustParse(tripID)
	}))
	pushServer.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			connectionClosed.Do(func() { close(pushConnectionClosed) })
		}
	}
	pushServer.Start()
	t.Cleanup(pushServer.Close)
	gateway, err := push.NewGateway(pushServer.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("create push gateway: %v", err)
	}
	t.Cleanup(gateway.CloseIdleConnections)

	telemetryService, resourcesClosed := newShutdownTelemetry(t, pool, pushConnectionClosed)
	queue, err := positions.NewQueue(2)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	metrics, err := positions.NewMetrics(metricnoop.NewMeterProvider(), queue)
	if err != nil {
		t.Fatalf("create worker metrics: %v", err)
	}
	queryStarted := make(chan struct{})
	repo := NewMocktripRepository(ctrl)
	gomock.InOrder(
		repo.EXPECT().ListStaleTrip(gomock.Any(), time.Minute).
			Return([]model.StaleTrip{running, pending}, nil),
		repo.EXPECT().ListStaleTrip(gomock.Any(), time.Minute).
			DoAndReturn(func(queryCtx context.Context, _ time.Duration) ([]model.StaleTrip, error) {
				close(queryStarted)
				<-queryCtx.Done()
				return nil, queryCtx.Err()
			}),
	)
	worker, err := positions.NewWorker(
		queue, 1, 5*time.Second, gateway, logger, repo, ticker, time.Minute,
		metrics, telemetryService.TracerProvider.Tracer("container.test"),
	)
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if shutdownErr := worker.Shutdown(cleanupCtx); shutdownErr != nil {
			t.Errorf("clean up worker: %v", shutdownErr)
		}
	})

	httpCtx, cancelHTTP := context.WithCancel(ctx)
	defer cancelHTTP()
	httpStarted := make(chan struct{})
	releaseHTTP := make(chan struct{})
	httpShutdownStarted := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(httpFinished)
		close(httpStarted)
		select {
		case <-releaseHTTP:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
			t.Error("HTTP request canceled during graceful shutdown")
		}
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return httpCtx }
	server.Config.RegisterOnShutdown(func() { close(httpShutdownStarted) })
	server.Start()
	t.Cleanup(server.Close)
	app := &Container{
		HTTPServer:  server.Config,
		Logger:      logger,
		pool:        pool,
		telemetry:   telemetryService,
		pushGateway: gateway, positionTicker: ticker, positionWorker: worker,
		cancelHTTP: cancelHTTP,
	}
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start application: %v", err)
	}
	sendTick(t, ticks)
	waitShutdownEvent(t, pushStarted)
	sendTick(t, ticks)
	waitShutdownEvent(t, queryStarted)

	responseDone := make(chan error, 1)
	go func() {
		response, requestErr := server.Client().Get(server.URL)
		if requestErr == nil {
			requestErr = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				requestErr = errors.New("HTTP request did not finish successfully")
			}
		}
		responseDone <- requestErr
	}()
	waitShutdownEvent(t, httpStarted)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- app.Close(shutdownCtx) }()
	waitShutdownEvent(t, httpShutdownStarted)

	connection, dialErr := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if dialErr == nil {
		_ = connection.Close()
		t.Error("HTTP server still accepts connections during shutdown")
	}
	close(releaseHTTP)
	if err := waitShutdownEvent(t, responseDone); err != nil {
		t.Fatalf("finish HTTP request: %v", err)
	}
	waitShutdownEvent(t, scannerStopped)
	select {
	case err := <-shutdownDone:
		t.Fatalf("application closed before workers drained: %v", err)
	default:
	}
	close(releasePush)
	if err := waitShutdownEvent(t, shutdownDone); err != nil {
		t.Fatalf("close application: %v", err)
	}
	for _, want := range []uuid.UUID{running.TripID, pending.TripID} {
		if got := waitShutdownEvent(t, pushFinished); got != want {
			t.Errorf("drained trip = %s, want %s", got, want)
		}
	}
	waitShutdownEvent(t, resourcesClosed)
}

func TestContainer_CloseDeadlineCancelsHTTPAndWorkers(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ticker := NewMockTicker(ctrl)
	ticks := make(chan time.Time)
	scannerStopped := make(chan struct{})
	ticker.EXPECT().C().Return((<-chan time.Time)(ticks))
	gomock.InOrder(
		ticker.EXPECT().Stop().Do(func() { close(scannerStopped) }),
		ticker.EXPECT().Stop(),
	)
	pushStarted := make(chan struct{})
	pushStopped := make(chan error, 1)
	staleTrip := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	gateway := NewMockpushGateway(ctrl)
	gateway.EXPECT().RequestPosition(gomock.Any(), staleTrip.DriverID, staleTrip.TripID).
		DoAndReturn(func(requestCtx context.Context, _, _ uuid.UUID) error {
			close(pushStarted)
			<-requestCtx.Done()
			pushStopped <- requestCtx.Err()
			return requestCtx.Err()
		})
	repo := NewMocktripRepository(ctrl)
	repo.EXPECT().ListStaleTrip(gomock.Any(), time.Minute).
		Return([]model.StaleTrip{staleTrip}, nil)
	queue, err := positions.NewQueue(1)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	metrics, err := positions.NewMetrics(metricnoop.NewMeterProvider(), queue)
	if err != nil {
		t.Fatalf("create worker metrics: %v", err)
	}
	telemetryService := &telemetry.Telemetry{
		TracerProvider: sdktrace.NewTracerProvider(),
		MeterProvider:  sdkmetric.NewMeterProvider(),
	}
	worker, err := positions.NewWorker(
		queue, 1, time.Minute, gateway, logger, repo, ticker, time.Minute,
		metrics, telemetryService.TracerProvider.Tracer("container.test"),
	)
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if shutdownErr := worker.Shutdown(cleanupCtx); shutdownErr != nil {
			t.Errorf("clean up worker: %v", shutdownErr)
		}
	})

	httpCtx, cancelHTTP := context.WithCancel(ctx)
	defer cancelHTTP()
	app := &Container{
		Logger: logger, telemetry: telemetryService,
		positionTicker: ticker, positionWorker: worker, cancelHTTP: cancelHTTP,
	}
	httpStarted := make(chan struct{})
	httpStopped := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(httpStarted)
		<-r.Context().Done()
		httpStopped <- r.Context().Err()
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return httpCtx }
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			app.httpConnections.Add(1)
		case http.StateClosed:
			app.httpConnections.Done()
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	app.HTTPServer = server.Config
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start application: %v", err)
	}
	sendTick(t, ticks)
	waitShutdownEvent(t, pushStarted)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, requestErr := server.Client().Get(server.URL)
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	waitShutdownEvent(t, httpStarted)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shutdownCancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- app.Close(shutdownCtx) }()
	if err := waitShutdownEvent(t, shutdownDone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline exceeded", err)
	}
	if err := waitShutdownEvent(t, httpStopped); !errors.Is(err, context.Canceled) {
		t.Errorf("HTTP request error = %v, want canceled", err)
	}
	if err := waitShutdownEvent(t, pushStopped); !errors.Is(err, context.Canceled) {
		t.Errorf("push request error = %v, want canceled", err)
	}
	waitShutdownEvent(t, scannerStopped)
	waitShutdownEvent(t, requestDone)
}

func newShutdownPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, error) {
	t.Helper()
	connectErr := errors.New("test database connection disabled")
	poolConfig, err := pgxpool.ParseConfig("postgres://localhost/tripgo?sslmode=disable")
	if err != nil {
		t.Fatalf("parse pool configuration: %v", err)
	}
	poolConfig.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		return connectErr
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, connectErr
}

func newShutdownTelemetry(t *testing.T, pool *pgxpool.Pool, pushConnectionClosed <-chan struct{}) (*telemetry.Telemetry, <-chan struct{}) {
	t.Helper()
	resourcesClosed := make(chan struct{})
	processor := NewMockSpanProcessor(gomock.NewController(t))
	processor.EXPECT().OnStart(gomock.Any(), gomock.Any()).AnyTimes()
	processor.EXPECT().OnEnd(gomock.Any()).AnyTimes()
	processor.EXPECT().Shutdown(gomock.Any()).
		DoAndReturn(func(shutdownCtx context.Context) error {
			connection, acquireErr := pool.Acquire(shutdownCtx)
			if connection != nil {
				connection.Release()
			}
			if !errors.Is(acquireErr, puddle.ErrClosedPool) {
				t.Errorf("database pool still open when shutting down telemetry: %v", acquireErr)
			}
			select {
			case <-pushConnectionClosed:
			case <-shutdownCtx.Done():
				t.Error("push connection still open when shutting down telemetry")
			}
			close(resourcesClosed)
			return nil
		})
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	return &telemetry.Telemetry{
		TracerProvider: tracerProvider,
		MeterProvider:  sdkmetric.NewMeterProvider(),
	}, resourcesClosed
}

func sendTick(t *testing.T, ticks chan<- time.Time) {
	t.Helper()
	select {
	case ticks <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatal("scanner did not receive a tick")
	}
}

func waitShutdownEvent[T any](t *testing.T, event <-chan T) T {
	t.Helper()
	select {
	case value := <-event:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for shutdown")
		var zero T
		return zero
	}
}
