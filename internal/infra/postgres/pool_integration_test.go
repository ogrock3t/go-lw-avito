//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/ogrock3t/go-lw-avito/internal/config"
)

func TestPool_QueryAndTransactionTelemetry(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
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
	pool, err := NewPool(ctx, config.DatabaseConfig{
		URL: databaseURL, MaxConns: 1, MaxConnLifetime: time.Hour, ConnectTimeout: 3 * time.Second,
	}, provider, meterProvider)
	if err != nil {
		t.Fatalf("create instrumented pool: %v", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	// A session-local table shadows any real trips table and disappears on close.
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE trips (id integer PRIMARY KEY)"); err != nil {
		t.Fatalf("create temporary table: %v", err)
	}
	ctx, parent := provider.Tracer("test").Start(ctx, "POST /api/v1/trips", trace.WithSpanKind(trace.SpanKindServer))
	defer parent.End()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil {
			t.Errorf("rollback transaction: %v", err)
		}
	}()
	if _, err := tx.Exec(ctx, "INSERT INTO trips (id) VALUES ($1)", 1); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var id int
	if err := tx.QueryRow(ctx, "SELECT id FROM trips WHERE id = $1", 1).Scan(&id); err != nil || id != 1 {
		t.Fatalf("select: id = %d, error = %v", id, err)
	}
	_, err = tx.Exec(ctx, "INSERT INTO trips (id) VALUES ($1)", 1)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolationType {
		t.Fatalf("duplicate insert error = %v, want 23505", err)
	}
	var failedInsert bool
	for _, span := range recorder.Ended() {
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "error.type" && attr.Value.AsString() == uniqueViolationType {
				assertDBSpan(t, span, parent.SpanContext(), "INSERT", tripsCollection, uniqueViolationType)
				failedInsert = true
			}
		}
	}
	if !failedInsert {
		t.Error("failed INSERT did not create an instrumented child span")
	}
	assertFailedInsertMetric(t, reader)
}

func assertFailedInsertMetric(t *testing.T, reader *sdkmetric.ManualReader) {
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
			if !ok {
				t.Fatalf("duration data = %#v", measurement.Data)
			}
			for _, point := range histogram.DataPoints {
				value, _ := point.Attributes.Value(attribute.Key("error.type"))
				if value.AsString() == uniqueViolationType {
					assertDBAttributes(t, point.Attributes, "INSERT", tripsCollection, uniqueViolationType)
					return
				}
			}
		}
	}
	t.Error("failed INSERT duration metric not found")
}
