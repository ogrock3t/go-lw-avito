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
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		w.runScanner(ctx)
	}()

	for i := 0; i < w.workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			w.runWorker(ctx)
		}()
	}

	wg.Wait()
}

func (w *Worker) CancelTrip(tripID uuid.UUID) {
	w.queue.CancelTrip(tripID)
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
