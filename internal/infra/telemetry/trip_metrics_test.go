package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestTripMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown metrics: %v", err)
		}
	})
	metrics, err := NewTripMetrics(provider)
	if err != nil {
		t.Fatalf("create trip metrics: %v", err)
	}
	ctx := context.Background()
	metrics.TripCreated(ctx)
	metrics.TripCreated(ctx)
	metrics.TripCompleted(ctx)
	assertTripCounters(t, reader, 2, 1)
	// Repeated collection must retain cumulative counts.
	assertTripCounters(t, reader, 2, 1)
	metrics.TripCompleted(ctx)
	assertTripCounters(t, reader, 2, 2)
}

func assertTripCounters(t *testing.T, reader *sdkmetric.ManualReader, created, completed int64) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	want := map[string]int64{
		"tripgo.trips.created":   created,
		"tripgo.trips.completed": completed,
	}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			value, ok := want[measurement.Name]
			if !ok {
				continue
			}
			sum, ok := measurement.Data.(metricdata.Sum[int64])
			if !ok || !sum.IsMonotonic || len(sum.DataPoints) != 1 {
				t.Fatalf("invalid counter %s: %#v", measurement.Name, measurement.Data)
			}
			point := sum.DataPoints[0]
			if point.Value != value || point.Attributes.Len() != 0 || measurement.Unit != "{trip}" {
				t.Errorf("counter %s: value = %d, attributes = %v, unit = %q", measurement.Name, point.Value, point.Attributes, measurement.Unit)
			}
			delete(want, measurement.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing counters: %v", want)
	}
}
