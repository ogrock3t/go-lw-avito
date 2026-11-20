package positions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

	metrics *Metrics
	tracer  trace.Tracer
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
	metrics *Metrics,
	tracer trace.Tracer,
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

	if metrics == nil {
		return nil, fmt.Errorf("position metrics are required")
	}

	if tracer == nil {
		return nil, fmt.Errorf("position tracer is required")
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
		metrics:        metrics,
		tracer:         tracer,
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

	options := []trace.SpanStartOption{
		trace.WithNewRoot(),
		trace.WithAttributes(
			attribute.String("trip_id", job.tripID.String()),
			attribute.String("task_id", job.id.String()),
		),
	}

	if job.scanContext.IsValid() {
		options = append(
			options,
			trace.WithLinks(trace.Link{
				SpanContext: job.scanContext,
			}),
		)
	}

	ctx, span := w.tracer.Start(ctx, "positions.process", options...)

	var requestErr error

	defer func() {
		result := requestResult(requestErr)

		w.metrics.requests.Add(
			ctx,
			1,
			metric.WithAttributes(
				attribute.String("result", result),
			),
		)

		span.SetAttributes(attribute.String("result", result))

		if requestErr != nil {
			span.RecordError(requestErr)
			span.SetStatus(codes.Error, "position request failed")
		}

		span.End()
	}()

	requestCtx, cancel := context.WithTimeout(ctx, w.requestTimeout)
	defer cancel()

	if !w.queue.start(job, cancel) {
		requestErr = context.Canceled
		return
	}

	requestErr = requestCtx.Err()
	if requestErr != nil {
		return
	}

	w.metrics.workersActive.Add(ctx, 1)
	defer w.metrics.workersActive.Add(ctx, -1)

	started := time.Now()

	requestErr = w.push.RequestPosition(
		requestCtx,
		job.driverID,
		job.tripID,
	)

	w.metrics.duration.Record(
		ctx,
		time.Since(started).Seconds(),
		metric.WithAttributes(
			attribute.String("result", requestResult(requestErr)),
		),
	)

	if requestErr != nil {
		w.logger.ErrorContext(
			ctx,
			"request trip position",
			slog.String("trip_id", job.tripID.String()),
			slog.String("task_id", job.id.String()),
			slog.Any("error", requestErr),
		)
	}
}

func requestResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "error"
	}
}
