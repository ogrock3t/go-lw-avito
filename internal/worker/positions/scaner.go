package positions

import (
	"context"
	"log/slog"
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
	w.queue.beginScan()
	defer w.queue.endScan()

	trips, err := w.repo.ListStaleTrip(ctx, w.staleAfter)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "list stale trips",
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
		})

		w.logger.DebugContext(ctx, "enqueue position request",
			slog.String("trip_id", trip.TripID.String()),
			slog.String("result", string(result)),
		)

		if result == resultDropped {
			w.logger.WarnContext(ctx, "position request dropped",
				slog.String("trip_id", trip.TripID.String()),
			)
		}

		if evicted != nil {
			w.logger.WarnContext(ctx, "position request evicted",
				slog.String("trip_id", evicted.tripID.String()),
				slog.String("task_id", evicted.id.String()),
			)
		}
	}
}
