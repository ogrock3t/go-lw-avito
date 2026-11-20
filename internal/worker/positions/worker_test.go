package positions_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	"github.com/ogrock3t/go-lw-avito/internal/model"
	"github.com/ogrock3t/go-lw-avito/internal/worker/positions"
)

func TestWorker_RepeatedTicksDoNotDuplicateQueuedOrRunningTrips(t *testing.T) {
	fixture := newWorkerFixture(t, 8, 1)
	running := staleTrip(30 * time.Minute)
	pending := staleTrip(20 * time.Minute)
	sentinel := staleTrip(10 * time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	drained := make(chan struct{})

	first := fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	second := fixture.push.EXPECT().
		RequestPosition(gomock.Any(), pending.DriverID, pending.TripID).
		Return(nil)
	last := fixture.push.EXPECT().
		RequestPosition(gomock.Any(), sentinel.DriverID, sentinel.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(drained)
			return nil
		})
	gomock.InOrder(first, second, last)

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)
	fixture.scan(t, []model.StaleTrip{pending})
	fixture.scan(t, []model.StaleTrip{running, pending})
	fixture.scan(t, []model.StaleTrip{sentinel})

	close(release)
	receive(t, drained)
}

func TestWorker_FullQueueDoesNotBlockScanner(t *testing.T) {
	fixture := newWorkerFixture(t, 1, 1)
	running := staleTrip(30 * time.Minute)
	pending := staleTrip(20 * time.Minute)
	dropped := staleTrip(10 * time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), pending.DriverID, pending.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(finished)
			return nil
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), dropped.DriverID, dropped.TripID).
		Times(0)

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)

	fixture.scan(t, []model.StaleTrip{pending, dropped})

	close(release)
	receive(t, finished)
}

func TestWorker_EvictionAndRejectionRecordDroppedMetrics(t *testing.T) {
	fixture := newWorkerFixture(t, 1, 1)
	running := staleTrip(time.Hour)
	newest := staleTrip(10 * time.Minute)
	oldest := staleTrip(30 * time.Minute)
	rejected := staleTrip(5 * time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), oldest.DriverID, oldest.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(finished)
			return nil
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), newest.DriverID, newest.TripID).
		Times(0)
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), rejected.DriverID, rejected.TripID).
		Times(0)

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)
	fixture.scan(t, []model.StaleTrip{newest})
	fixture.scan(t, []model.StaleTrip{oldest})
	assertQueueDropMetrics(t, fixture.metricsReader, 3, 1)

	fixture.scan(t, []model.StaleTrip{rejected})
	assertQueueDropMetrics(t, fixture.metricsReader, 3, 2)

	close(release)
	receive(t, finished)
}

func assertQueueDropMetrics(t *testing.T, reader *sdkmetric.ManualReader, queued, dropped int64) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect worker metrics: %v", err)
	}
	counters := make(map[string]int64)
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			sum, ok := measurement.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				result, ok := point.Attributes.Value(attribute.Key("result"))
				if ok {
					counters[measurement.Name+"/"+result.AsString()] += point.Value
				}
			}
		}
	}
	want := map[string]int64{
		"tripgo.stale_checks/queued":       queued,
		"tripgo.stale_checks/dropped":      dropped,
		"tripgo.position_requests/dropped": dropped,
	}
	for name, value := range want {
		if got := counters[name]; got != value {
			t.Errorf("counter %s = %d, want %d", name, got, value)
		}
	}
}

func TestWorker_CancelQueuedTripSkipsPush(t *testing.T) {
	fixture := newWorkerFixture(t, 2, 1)
	running := staleTrip(30 * time.Minute)
	canceled := staleTrip(20 * time.Minute)
	sentinel := staleTrip(10 * time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	drained := make(chan struct{})

	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), canceled.DriverID, canceled.TripID).
		Times(0)
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), sentinel.DriverID, sentinel.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(drained)
			return nil
		})

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)
	fixture.scan(t, []model.StaleTrip{canceled})
	fixture.worker.CancelTrip(canceled.TripID)
	fixture.scan(t, []model.StaleTrip{sentinel})

	close(release)
	receive(t, drained)
}

func TestWorker_CancellationDuringQueryRejectsStaleResult(t *testing.T) {
	fixture := newWorkerFixture(t, 2, 1)
	canceled := staleTrip(20 * time.Minute)
	unaffected := staleTrip(10 * time.Minute)
	selected := make(chan struct{})
	releaseQuery := make(chan struct{})
	requested := make(chan uuid.UUID, 2)

	fixture.repo.EXPECT().
		ListStaleTrip(gomock.Any(), time.Minute).
		DoAndReturn(func(ctx context.Context, _ time.Duration) ([]model.StaleTrip, error) {
			close(selected)
			select {
			case <-releaseQuery:
				return []model.StaleTrip{canceled, unaffected}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), unaffected.DriverID, unaffected.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			requested <- unaffected.TripID
			return nil
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), canceled.DriverID, canceled.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			requested <- canceled.TripID
			return nil
		})

	fixture.start(t)
	fixture.tick(t)
	receive(t, selected)
	fixture.worker.CancelTrip(canceled.TripID)
	close(releaseQuery)

	if got := receive(t, requested); got != unaffected.TripID {
		t.Fatalf("first requested trip = %s, want unaffected trip %s", got, unaffected.TripID)
	}

	fixture.scan(t, []model.StaleTrip{canceled})
	if got := receive(t, requested); got != canceled.TripID {
		t.Errorf("next scan requested trip = %s, want %s", got, canceled.TripID)
	}
}

func TestWorker_FailedScanClearsCancellations(t *testing.T) {
	fixture := newWorkerFixture(t, 1, 1)
	trip := staleTrip(time.Minute)
	finished := make(chan struct{})

	fixture.repo.EXPECT().
		ListStaleTrip(gomock.Any(), time.Minute).
		DoAndReturn(func(context.Context, time.Duration) ([]model.StaleTrip, error) {
			fixture.worker.CancelTrip(trip.TripID)
			return nil, errors.New("database unavailable")
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), trip.DriverID, trip.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(finished)
			return nil
		})

	fixture.start(t)
	fixture.tick(t)
	fixture.scan(t, []model.StaleTrip{trip})
	receive(t, finished)
}

func TestWorker_ContinuesAfterPushError(t *testing.T) {
	fixture := newWorkerFixture(t, 2, 1)
	firstTrip := staleTrip(20 * time.Minute)
	nextTrip := staleTrip(10 * time.Minute)
	finished := make(chan struct{})

	first := fixture.push.EXPECT().
		RequestPosition(gomock.Any(), firstTrip.DriverID, firstTrip.TripID).
		Return(errors.New("push service unavailable"))
	second := fixture.push.EXPECT().
		RequestPosition(gomock.Any(), nextTrip.DriverID, nextTrip.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(finished)
			return nil
		})
	gomock.InOrder(first, second)

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{firstTrip, nextTrip})
	receive(t, finished)
}

func TestWorker_ProcessesAllTasksWithConfiguredConcurrency(t *testing.T) {
	const workers = 3
	const taskCount = 12
	fixture := newWorkerFixture(t, taskCount, workers)
	trips := make([]model.StaleTrip, taskCount)
	wantDrivers := make(map[uuid.UUID]uuid.UUID, taskCount)
	for i := range trips {
		trips[i] = staleTrip(time.Duration(i+1) * time.Minute)
		wantDrivers[trips[i].TripID] = trips[i].DriverID
	}
	started := make(chan uuid.UUID, taskCount)
	finished := make(chan uuid.UUID, taskCount)
	release := make(chan struct{})
	var mu sync.Mutex
	active := 0
	maxActive := 0

	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(taskCount).
		DoAndReturn(func(ctx context.Context, driverID, tripID uuid.UUID) error {
			if driver, ok := wantDrivers[tripID]; !ok || driver != driverID {
				t.Errorf("push recipient = %s for trip %s, want %s", driverID, tripID, driver)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Error("push request context has no timeout")
			}

			mu.Lock()
			active++
			maxActive = max(maxActive, active)
			mu.Unlock()
			started <- tripID

			select {
			case <-release:
			case <-ctx.Done():
			}

			mu.Lock()
			active--
			mu.Unlock()
			finished <- tripID
			return ctx.Err()
		})

	fixture.start(t)
	fixture.scan(t, trips)
	for i := 0; i < workers; i++ {
		receive(t, started)
	}
	mu.Lock()
	initialActive := active
	mu.Unlock()
	if initialActive != workers {
		t.Fatalf("concurrent requests = %d, want %d", initialActive, workers)
	}

	close(release)
	seen := make(map[uuid.UUID]bool, taskCount)
	for i := 0; i < taskCount; i++ {
		tripID := receive(t, finished)
		if seen[tripID] {
			t.Errorf("trip %s processed twice", tripID)
		}
		seen[tripID] = true
	}
	mu.Lock()
	peak := maxActive
	mu.Unlock()
	if peak != workers {
		t.Errorf("peak concurrent requests = %d, want %d", peak, workers)
	}
	if len(seen) != taskCount {
		t.Errorf("processed trips = %d, want %d", len(seen), taskCount)
	}
}

func TestWorker_CancellationStopsScannerAndActiveRequests(t *testing.T) {
	fixture := newWorkerFixture(t, 2, 2)
	started := make(chan struct{}, 2)
	stopped := make(chan error, 2)
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(2).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			started <- struct{}{}
			<-ctx.Done()
			stopped <- ctx.Err()
			return ctx.Err()
		})

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{staleTrip(time.Minute), staleTrip(2 * time.Minute)})
	receive(t, started)
	receive(t, started)
	fixture.cancel()

	receive(t, fixture.done)
	for i := 0; i < 2; i++ {
		if err := receive(t, stopped); !errors.Is(err, context.Canceled) {
			t.Errorf("request error = %v, want context.Canceled", err)
		}
	}
}

func TestWorker_ShutdownStopsScannerAndDrainsQueuedTasks(t *testing.T) {
	fixture := newWorkerFixture(t, 2, 1)
	running := staleTrip(time.Hour)
	pending := []model.StaleTrip{staleTrip(30 * time.Minute), staleTrip(20 * time.Minute)}
	started := make(chan struct{})
	release := make(chan struct{})
	processed := make(chan uuid.UUID, 3)
	requestErrors := make(chan error, 3)

	first := fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, tripID uuid.UUID) error {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			processed <- tripID
			requestErrors <- ctx.Err()
			return ctx.Err()
		})
	for _, trip := range pending {
		first = fixture.push.EXPECT().
			RequestPosition(gomock.Any(), trip.DriverID, trip.TripID).
			After(first).
			DoAndReturn(func(ctx context.Context, _, tripID uuid.UUID) error {
				processed <- tripID
				requestErrors <- ctx.Err()
				return ctx.Err()
			})
	}

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)
	fixture.scan(t, pending)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- fixture.worker.Shutdown(shutdownCtx)
	}()

	receive(t, fixture.scannerStopped)
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before the active request finished: %v", err)
	default:
	}

	close(release)
	if err := receive(t, shutdownDone); err != nil {
		t.Fatalf("shutdown worker: %v", err)
	}
	receive(t, fixture.done)

	for _, want := range []uuid.UUID{running.TripID, pending[0].TripID, pending[1].TripID} {
		if got := receive(t, processed); got != want {
			t.Errorf("processed trip = %s, want %s", got, want)
		}
		if err := receive(t, requestErrors); err != nil {
			t.Errorf("request canceled during graceful shutdown: %v", err)
		}
	}
}

func TestWorker_ShutdownDeadlineCancelsActiveRequestAndSkipsQueuedTasks(t *testing.T) {
	fixture := newWorkerFixture(t, 1, 1)
	running := staleTrip(time.Hour)
	pending := staleTrip(30 * time.Minute)
	started := make(chan struct{})
	requestStopped := make(chan error, 1)
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			<-ctx.Done()
			requestStopped <- ctx.Err()
			return ctx.Err()
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), pending.DriverID, pending.TripID).
		Times(0)

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)
	fixture.scan(t, []model.StaleTrip{pending})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- fixture.worker.Shutdown(shutdownCtx)
	}()

	receive(t, fixture.scannerStopped)
	if err := receive(t, shutdownDone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v, want deadline exceeded", err)
	}
	if err := receive(t, requestStopped); !errors.Is(err, context.Canceled) {
		t.Errorf("active request error = %v, want canceled", err)
	}
	receive(t, fixture.done)
}

func TestWorker_ScannerRecoversAfterRepositoryError(t *testing.T) {
	fixture := newWorkerFixture(t, 1, 1)
	trip := staleTrip(time.Minute)
	finished := make(chan struct{})
	fixture.repo.EXPECT().
		ListStaleTrip(gomock.Any(), time.Minute).
		Return(nil, errors.New("database unavailable"))
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), trip.DriverID, trip.TripID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(finished)
			return nil
		})

	fixture.start(t)
	fixture.tick(t)
	fixture.scan(t, []model.StaleTrip{trip})
	receive(t, finished)
}

func TestWorker_PrioritySurvivesScannerInputOrder(t *testing.T) {
	fixture := newWorkerFixture(t, 4, 1)
	running := staleTrip(time.Hour)
	oldest := staleTrip(30 * time.Minute)
	middle := staleTrip(20 * time.Minute)
	newest := staleTrip(10 * time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	processed := make(chan uuid.UUID, 3)
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), running.DriverID, running.TripID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	fixture.push.EXPECT().
		RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(3).
		DoAndReturn(func(_ context.Context, _, tripID uuid.UUID) error {
			processed <- tripID
			return nil
		})

	fixture.start(t)
	fixture.scan(t, []model.StaleTrip{running})
	receive(t, started)
	fixture.scan(t, []model.StaleTrip{middle, newest, oldest})
	close(release)

	got := []uuid.UUID{receive(t, processed), receive(t, processed), receive(t, processed)}
	want := []uuid.UUID{oldest.TripID, middle.TripID, newest.TripID}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("push order = %v, want %v", got, want)
	}
}

type workerFixture struct {
	worker         *positions.Worker
	metricsReader  *sdkmetric.ManualReader
	repo           *MocktripRepository
	push           *MockpushGateway
	ticks          chan time.Time
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	scannerStopped chan struct{}
}

func newWorkerFixture(t *testing.T, capacity, workers int) *workerFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	push := NewMockpushGateway(ctrl)
	ticker := NewMockTicker(ctrl)
	ticks := make(chan time.Time)
	scannerStopped := make(chan struct{})
	ticker.EXPECT().C().Return((<-chan time.Time)(ticks))
	ticker.EXPECT().Stop().Do(func() {
		close(scannerStopped)
	})
	queue, err := positions.NewQueue(capacity)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	metricsReader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricsReader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown worker metrics: %v", err)
		}
	})
	metrics, err := positions.NewMetrics(provider, queue)
	if err != nil {
		t.Fatalf("create position metrics: %v", err)
	}
	tracer := tracenoop.NewTracerProvider().Tracer("positions.test")

	worker, err := positions.NewWorker(
		queue,
		workers,
		time.Minute,
		push,
		logger,
		repo,
		ticker,
		time.Minute,
		metrics,
		tracer,
	)
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &workerFixture{
		worker:         worker,
		metricsReader:  metricsReader,
		repo:           repo,
		push:           push,
		ticks:          ticks,
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		scannerStopped: scannerStopped,
	}
}

func (f *workerFixture) start(t *testing.T) {
	t.Helper()
	go func() {
		defer close(f.done)
		f.worker.Run(f.ctx)
	}()
	t.Cleanup(func() {
		f.cancel()
		receive(t, f.done)
	})
}

func (f *workerFixture) scan(t *testing.T, trips []model.StaleTrip) {
	t.Helper()
	scanned := make(chan struct{})
	scan := f.repo.EXPECT().
		ListStaleTrip(gomock.Any(), time.Minute).
		Return(trips, nil)
	barrier := f.repo.EXPECT().
		ListStaleTrip(gomock.Any(), time.Minute).
		DoAndReturn(func(context.Context, time.Duration) ([]model.StaleTrip, error) {
			close(scanned)
			return nil, nil
		})
	gomock.InOrder(scan, barrier)

	f.tick(t)
	f.tick(t)
	receive(t, scanned)
}

func (f *workerFixture) tick(t *testing.T) {
	t.Helper()
	select {
	case f.ticks <- time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC):
	case <-time.After(5 * time.Second):
		t.Fatal("scanner did not receive a tick")
	}
}

func staleTrip(age time.Duration) model.StaleTrip {
	return model.StaleTrip{
		TripID:         uuid.New(),
		DriverID:       uuid.New(),
		LastActivityAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Add(-age),
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for test synchronization")
		var zero T
		return zero
	}
}
