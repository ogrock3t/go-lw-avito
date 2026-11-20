package positions

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"log/slog"
	"sync"
	"time"
)

type Worker struct {
	queue          *Queue
	push           pushGateway
	workers        int
	requestTimeout time.Duration
	logger         *slog.Logger

	repo       tripRepository
	ticker     Ticker
	staleAfter time.Duration

	lifecycleMu sync.Mutex
	started     bool

	cancelScanner context.CancelFunc
	cancelWorkers context.CancelFunc
	done          chan struct{}
}

func NewWorker(
	queue *Queue,
	workers int,
	requestTimeout time.Duration,
	push pushGateway,
	logger *slog.Logger,
	repo tripRepository,
	ticker Ticker,
	staleAfter time.Duration,
) (*Worker, error) {
	if workers <= 0 {
		return nil, fmt.Errorf("worker count must be positive, got %d", workers)
	}

	if requestTimeout <= 0 {
		return nil, fmt.Errorf("request timeout must be positive, got %s", requestTimeout)
	}

	if staleAfter <= 0 {
		return nil, fmt.Errorf("stale threshold must be positive, got %s", staleAfter)
	}

	if queue == nil {
		return nil, fmt.Errorf("position queue is required")
	}

	return &Worker{
		queue:          queue,
		push:           push,
		workers:        workers,
		requestTimeout: requestTimeout,
		logger:         logger,
		repo:           repo,
		ticker:         ticker,
		staleAfter:     staleAfter,
	}, nil
}

func (w *Worker) Run(ctx context.Context) {
	if err := w.Start(ctx); err != nil {
		w.logger.ErrorContext(
			ctx,
			"start position worker",
			slog.Any("error", err),
		)
		return
	}

	<-w.done
}

func (w *Worker) CancelTrip(tripID uuid.UUID) {
	w.queue.CancelTrip(tripID)
}

func (w *Worker) Start(ctx context.Context) error {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	if w.started {
		return fmt.Errorf("position worker already started")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start position worker: %w", err)
	}

	scannerCtx, cancelScanner := context.WithCancel(ctx)
	workersCtx, cancelWorkers := context.WithCancel(ctx)

	w.cancelScanner = cancelScanner
	w.cancelWorkers = cancelWorkers
	w.done = make(chan struct{})
	w.started = true

	var wg sync.WaitGroup
	wg.Add(1 + w.workers)

	go func() {
		defer wg.Done()
		defer w.queue.Close()

		w.runScanner(scannerCtx)
	}()

	for i := 0; i < w.workers; i++ {
		go func() {
			defer wg.Done()

			w.runWorker(workersCtx)
		}()
	}

	go func() {
		wg.Wait()

		cancelScanner()
		cancelWorkers()
		close(w.done)
	}()

	return nil
}

func (w *Worker) Shutdown(ctx context.Context) error {
	w.lifecycleMu.Lock()

	if !w.started {
		w.lifecycleMu.Unlock()
		return nil
	}

	cancelScanner := w.cancelScanner
	cancelWorkers := w.cancelWorkers
	done := w.done

	w.lifecycleMu.Unlock()

	cancelScanner()

	select {
	case <-done:
		return nil

	case <-ctx.Done():
		cancelWorkers()
		<-done

		return fmt.Errorf("shutdown position worker: %w", ctx.Err())
	}
}

func (w *Worker) runWorker(ctx context.Context) {
	for {
		job, ok := w.queue.dequeue(ctx)
		if !ok {
			return
		}

		w.process(ctx, job)
	}
}

func (w *Worker) process(ctx context.Context, job task) {
	defer w.queue.complete(job)

	requestCtx, cancel := context.WithTimeout(ctx, w.requestTimeout)
	defer cancel()

	if !w.queue.start(job, cancel) {
		return
	}

	if requestCtx.Err() != nil {
		return
	}

	err := w.push.RequestPosition(requestCtx, job.driverID, job.tripID)
	if err != nil {
		w.logger.ErrorContext(ctx, "request trip position",
			slog.String("trip_id", job.tripID.String()),
			slog.String("task_id", job.id.String()),
			slog.Any("error", err),
		)
	}
}
