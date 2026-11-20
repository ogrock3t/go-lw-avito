package positions

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

type Metrics struct {
	staleChecks   metric.Int64Counter
	requests      metric.Int64Counter
	duration      metric.Float64Histogram
	workersActive metric.Int64UpDownCounter
}

func NewMetrics(provider metric.MeterProvider, queue *Queue) (*Metrics, error) {
	if queue == nil {
		return nil, fmt.Errorf("position queue is required for metrics")
	}

	meter := provider.Meter("github.com/ogrock3t/go-lw-avito/internal/worker/positions")

	staleChecks, err := meter.Int64Counter("tripgo.stale_checks")
	if err != nil {
		return nil, fmt.Errorf("create stale checks counter: %w", err)
	}

	requests, err := meter.Int64Counter("tripgo.position_requests")
	if err != nil {
		return nil, fmt.Errorf("create position requests counter: %w", err)
	}

	duration, err := meter.Float64Histogram(
		"tripgo.position_requests.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create position requests duration: %w", err)
	}

	workersActive, err := meter.Int64UpDownCounter("tripgo.position_requests.workers.active")
	if err != nil {
		return nil, fmt.Errorf("create active workers counter: %w", err)
	}

	_, err = meter.Int64ObservableGauge(
		"tripgo.position_requests.queue.size",
		metric.WithInt64Callback(
			func(_ context.Context, observer metric.Int64Observer) error {
				observer.Observe(int64(queue.Len()))
				return nil
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create position queue size gauge: %w", err)
	}

	workersActive.Add(context.Background(), 0)

	return &Metrics{
		staleChecks:   staleChecks,
		requests:      requests,
		duration:      duration,
		workersActive: workersActive,
	}, nil
}
