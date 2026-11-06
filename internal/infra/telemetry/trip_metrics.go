package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

type TripMetrics struct {
	created   metric.Int64Counter
	completed metric.Int64Counter
}

func NewTripMetrics(provider metric.MeterProvider) (*TripMetrics, error) {
	meter := provider.Meter("github.com/ogrock3t/go-lw-avito/internal/infra/telemetry")
	created, err := meter.Int64Counter(
		"tripgo.trips.created",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Number of successfully created trips"),
	)
	if err != nil {
		return nil, fmt.Errorf("create trips created counter: %w", err)
	}
	completed, err := meter.Int64Counter(
		"tripgo.trips.completed",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Number of successfully completed trips"),
	)
	if err != nil {
		return nil, fmt.Errorf("create trips completed counter: %w", err)
	}
	return &TripMetrics{created: created, completed: completed}, nil
}

func (m *TripMetrics) TripCreated(ctx context.Context) {
	m.created.Add(ctx, 1)
}

func (m *TripMetrics) TripCompleted(ctx context.Context) {
	m.completed.Add(ctx, 1)
}
