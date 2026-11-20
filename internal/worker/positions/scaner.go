package positions

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

func (w *Worker) runScanner(ctx context.Context) {
	defer w.ticker.Stop()

	ticks := w.ticker.C()

	for {
		select {
		case <-ctx.Done():
			return

		case _, ok := <-ticks:
			if !ok {
				return
			}

			w.scan(ctx)
		}
	}
}

func (w *Worker) scan(ctx context.Context) {
	ctx, span := w.tracer.Start(ctx, "positions.scan")
	defer span.End()

	w.queue.beginScan()
	defer w.queue.endScan()

	trips, err := w.repo.ListStaleTrip(ctx, w.staleAfter)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list stale trips failed")

		if ctx.Err() == nil {
			w.logger.ErrorContext(
				ctx,
				"list stale trips",
				slog.Any("error", err),
			)
		}

		return
	}

	for _, trip := range trips {
		if ctx.Err() != nil {
			return
		}

		result, evicted := w.queue.tryEnqueue(task{
			tripID:         trip.TripID,
			driverID:       trip.DriverID,
			lastActivityAt: trip.LastActivityAt,
			scanContext:    span.SpanContext(),
		})

		w.metrics.staleChecks.Add(
			ctx,
			1,
			metric.WithAttributes(
				attribute.String("result", string(result)),
			),
		)

		w.logger.DebugContext(
			ctx,
			"enqueue position request",
			slog.String("trip_id", trip.TripID.String()),
			slog.String("result", string(result)),
		)

		if result == resultDropped {
			w.metrics.requests.Add(
				ctx,
				1,
				metric.WithAttributes(
					attribute.String("result", "dropped"),
				),
			)

			w.logger.WarnContext(
				ctx,
				"position request dropped",
				slog.String("trip_id", trip.TripID.String()),
			)
		}

		if evicted != nil {
			w.metrics.staleChecks.Add(
				ctx,
				1,
				metric.WithAttributes(
					attribute.String("result", "dropped"),
				),
			)

			w.metrics.requests.Add(
				ctx,
				1,
				metric.WithAttributes(
					attribute.String("result", "dropped"),
				),
			)

			w.logger.WarnContext(
				ctx,
				"position request evicted",
				slog.String("trip_id", evicted.tripID.String()),
				slog.String("task_id", evicted.id.String()),
			)
		}
	}
}
