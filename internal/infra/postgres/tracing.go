package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const operationDuration = "db.client.operation.duration"

type queryAttributesKey struct{}

type queryTracer struct {
	*otelpgx.Tracer
}

func newQueryTracer(tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) *queryTracer {
	return &queryTracer{Tracer: otelpgx.NewTracer(
		otelpgx.WithTracerProvider(tracerProvider),
		otelpgx.WithMeterProvider(&queryMeterProvider{MeterProvider: meterProvider}),
		otelpgx.WithMetricOperationName(otelpgx.SQLOperationName),
		otelpgx.WithDisableSQLStatementInAttributes(),
	)}
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)
	return withQueryAttributes(ctx, data.SQL)
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	t.Tracer.TraceQueryEnd(withQueryError(ctx, data.Err), conn, data)
}

func (t *queryTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	ctx = t.Tracer.TracePrepareStart(ctx, conn, data)
	return withQueryAttributes(ctx, data.SQL)
}

func (t *queryTracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	t.Tracer.TracePrepareEnd(withQueryError(ctx, data.Err), conn, data)
}

func withQueryAttributes(ctx context.Context, statement string) context.Context {
	attributes := []attribute.KeyValue{
		attribute.String("db.operation.name", otelpgx.SQLOperationName(ctx, statement)),
	}
	if collection := queryCollection(statement); collection != "" {
		attributes = append(attributes, attribute.String("db.collection.name", collection))
	}
	trace.SpanFromContext(ctx).SetAttributes(attributes...)
	return context.WithValue(ctx, queryAttributesKey{}, attributes)
}

func withQueryError(ctx context.Context, err error) context.Context {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return ctx
	}
	errorType := reflect.TypeOf(err).String()
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		errorType = pgErr.Code
	case errors.Is(err, context.DeadlineExceeded):
		errorType = "timeout"
	case errors.Is(err, context.Canceled):
		errorType = "cancelled"
	}
	attr := attribute.String("error.type", errorType)
	trace.SpanFromContext(ctx).SetAttributes(attr)
	attributes, _ := ctx.Value(queryAttributesKey{}).([]attribute.KeyValue)
	attributes = append(append([]attribute.KeyValue{}, attributes...), attr)
	return context.WithValue(ctx, queryAttributesKey{}, attributes)
}

func queryCollection(statement string) string {
	words := strings.Fields(statement)
	for index := 0; index+1 < len(words); index++ {
		switch strings.ToUpper(words[index]) {
		case "FROM", "INTO", "UPDATE":
			name, _, _ := strings.Cut(words[index+1], "(")
			name = strings.ReplaceAll(name, `"`, "")
			name = strings.TrimPrefix(name, "public.")
			switch name {
			case "trips", "trip_status_history":
				return name
			}
		}
	}
	return ""
}

// Keep otelpgx's histogram and timing, enriching its measurements from the
// per-query context even when tracing is disabled by sampling.
type queryMeterProvider struct {
	metric.MeterProvider
}

func (p *queryMeterProvider) Meter(name string, options ...metric.MeterOption) metric.Meter {
	return &queryMeter{Meter: p.MeterProvider.Meter(name, options...)}
}

type queryMeter struct {
	metric.Meter
}

func (m *queryMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	histogram, err := m.Meter.Float64Histogram(name, options...)
	if err != nil || name != operationDuration {
		return histogram, err
	}
	return &queryHistogram{Float64Histogram: histogram}, nil
}

type queryHistogram struct {
	metric.Float64Histogram
}

func (h *queryHistogram) Record(ctx context.Context, value float64, options ...metric.RecordOption) {
	if attributes, ok := ctx.Value(queryAttributesKey{}).([]attribute.KeyValue); ok {
		options = append(options, metric.WithAttributes(attributes...))
	}
	h.Float64Histogram.Record(ctx, value, options...)
}
