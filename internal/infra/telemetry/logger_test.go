package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestLoggerLevels(t *testing.T) {
	for _, name := range []string{"debug", "info", "warn", "error"} {
		t.Run(name, func(t *testing.T) {
			logger, err := newLogger("trip-service", name)
			if err != nil {
				t.Fatalf("create logger: %v", err)
			}
			var level slog.Level
			if err := level.UnmarshalText([]byte(name)); err != nil {
				t.Fatal(err)
			}
			if !logger.Enabled(context.Background(), level) || logger.Enabled(context.Background(), level-1) {
				t.Errorf("incorrect level filter for %s", name)
			}
		})
	}
	if _, err := newLogger("trip-service", "invalid"); err == nil {
		t.Error("invalid log level accepted")
	}
}

func TestTraceHandler(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID,
	}))
	var logs bytes.Buffer
	logger := slog.New(&traceHandler{Handler: slog.NewJSONHandler(&logs, nil)}).With("service", "trip-service")
	logger.InfoContext(ctx, "business event", "trip_id", "123", "trace_id", "stale", "span_id", "stale")
	logger.WithGroup("operation").With("name", "save").InfoContext(ctx, "grouped event")
	logger.InfoContext(context.Background(), "background event")

	decoder := json.NewDecoder(&logs)
	for index := range 3 {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if record["service"] != "trip-service" {
			t.Errorf("record has no service: %v", record)
		}
		fields := record
		if index == 1 {
			var ok bool
			fields, ok = record["operation"].(map[string]any)
			if !ok || fields["name"] != "save" {
				t.Fatalf("grouped attributes lost: %v", record)
			}
		}
		if index < 2 && (fields["trace_id"] != traceID.String() || fields["span_id"] != spanID.String()) {
			t.Errorf("incorrect correlation: %v", record)
		}
		if index == 2 && (fields["trace_id"] != nil || fields["span_id"] != nil) {
			t.Errorf("span IDs leaked into background record: %v", record)
		}
	}
}
