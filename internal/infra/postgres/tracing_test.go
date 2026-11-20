package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	selectOperation     = "SELECT"
	selectTripsSQL      = "SELECT * FROM trips"
	uniqueViolationType = "23505"
	tripsCollection     = "trips"
)

func TestQueryTelemetry(t *testing.T) {
	tests := []struct {
		name       string
		statement  string
		operation  string
		collection string
		err        error
		errorType  string
		prepare    bool
		unsampled  bool
	}{
		{name: "select", statement: "SELECT * FROM trips WHERE id = $1", operation: selectOperation, collection: tripsCollection},
		{name: "insert", statement: "INSERT INTO trip_status_history (trip_id) VALUES ($1)", operation: "INSERT", collection: "trip_status_history"},
		{name: "update", statement: `UPDATE "public"."trips" SET status = $1`, operation: "UPDATE", collection: tripsCollection},
		{name: "transaction", statement: "commit", operation: "COMMIT"},
		{name: "SQL error", statement: "INSERT INTO trips (id) VALUES ($1)", operation: "INSERT", collection: tripsCollection, err: &pgconn.PgError{Code: uniqueViolationType, Message: "duplicate key"}, errorType: uniqueViolationType},
		{name: "timeout", statement: selectTripsSQL, operation: selectOperation, collection: tripsCollection, err: context.DeadlineExceeded, errorType: "timeout"},
		{name: "cancelled", statement: selectTripsSQL, operation: selectOperation, collection: tripsCollection, err: context.Canceled, errorType: "cancelled"},
		{name: "not found", statement: selectTripsSQL, operation: selectOperation, collection: tripsCollection, err: pgx.ErrNoRows},
		{name: "other error", statement: selectTripsSQL, operation: selectOperation, collection: tripsCollection, err: errors.New("connection closed"), errorType: "*errors.errorString"},
		{name: "prepare", statement: selectTripsSQL, operation: selectOperation, collection: tripsCollection, prepare: true},
		{name: "unsampled", statement: selectTripsSQL, operation: selectOperation, collection: tripsCollection, unsampled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			sampler := sdktrace.AlwaysSample()
			if test.unsampled {
				sampler = sdktrace.NeverSample()
			}
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sampler))
			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() {
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Errorf("shutdown tracing: %v", err)
				}
				if err := meterProvider.Shutdown(context.Background()); err != nil {
					t.Errorf("shutdown metrics: %v", err)
				}
			})
			ctx, parent := provider.Tracer("test").Start(context.Background(), "HTTP request")
			defer parent.End()
			tracer := newQueryTracer(provider, meterProvider)
			if test.prepare {
				queryCtx := tracer.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{SQL: test.statement})
				tracer.TracePrepareEnd(queryCtx, nil, pgx.TracePrepareEndData{Err: test.err})
			} else {
				queryCtx := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: test.statement, Args: []any{"private parameter"}})
				tracer.TraceQueryEnd(queryCtx, nil, pgx.TraceQueryEndData{Err: test.err})
			}
			if !test.unsampled {
				spans := recorder.Ended()
				if len(spans) != 1 {
					t.Fatalf("DB spans = %d, want 1", len(spans))
				}
				assertDBSpan(t, spans[0], parent.SpanContext(), test.operation, test.collection, test.errorType)
			}
			assertDBMetric(t, reader, test.operation, test.collection, test.errorType)
		})
	}
}

func assertDBSpan(t *testing.T, span sdktrace.ReadOnlySpan, parent trace.SpanContext, operation, collection, errorType string) {
	t.Helper()
	if span.Parent().SpanID() != parent.SpanID() || span.SpanContext().TraceID() != parent.TraceID() {
		t.Error("DB span is not a child of the request")
	}
	if span.SpanKind() != trace.SpanKindClient {
		t.Errorf("span kind = %v, want client", span.SpanKind())
	}
	attributes := attribute.NewSet(span.Attributes()...)
	assertDBAttributes(t, attributes, operation, collection, errorType)
	for _, key := range []string{"db.query.text", "db.statement", "pgx.query.parameters"} {
		if attributes.HasValue(attribute.Key(key)) {
			t.Errorf("span contains SQL or parameters: %s", key)
		}
	}
	wantStatus := codes.Unset
	if errorType != "" {
		wantStatus = codes.Error
	}
	if span.Status().Code != wantStatus {
		t.Errorf("span status = %v, want %v", span.Status().Code, wantStatus)
	}
}

func assertDBMetric(t *testing.T, reader *sdkmetric.ManualReader, operation, collection, errorType string) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != operationDuration {
				continue
			}
			histogram, ok := measurement.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 {
				t.Fatalf("duration data = %#v", measurement.Data)
			}
			point := histogram.DataPoints[0]
			if measurement.Unit != "s" || point.Count != 1 || point.Sum <= 0 {
				t.Errorf("invalid duration: unit = %q, count = %d, sum = %g", measurement.Unit, point.Count, point.Sum)
			}
			assertDBAttributes(t, point.Attributes, operation, collection, errorType)
			return
		}
	}
	t.Fatal("DB duration metric not found")
}

func assertDBAttributes(t *testing.T, attrs attribute.Set, operation, collection, errorType string) {
	t.Helper()
	for key, want := range map[string]string{
		"db.operation.name":  operation,
		"db.collection.name": collection,
		"error.type":         errorType,
	} {
		value, exists := attrs.Value(attribute.Key(key))
		if want == "" {
			if exists {
				t.Errorf("unexpected attribute %s = %v", key, value)
			}
		} else if !exists || value.AsString() != want {
			t.Errorf("attribute %s = %v, want %q", key, value, want)
		}
	}
}
