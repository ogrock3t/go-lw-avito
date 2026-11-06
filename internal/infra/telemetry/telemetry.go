package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/ogrock3t/go-lw-avito/internal/config"
)

type Telemetry struct {
	Logger         *slog.Logger
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
}

func New(ctx context.Context, cfg config.TelemetryConfig, logLevel string) (*Telemetry, error) {
	if cfg.ExporterProtocol != config.OTLPProtocolHTTPProtobuf {
		return nil, fmt.Errorf("unsupported OTLP protocol %q", cfg.ExporterProtocol)
	}

	logger, err := newLogger(cfg.ServiceName, logLevel)
	if err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}

	res, err := newResource(cfg)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	sampler, err := newSampler(cfg.TracesSampler)
	if err != nil {
		return nil, fmt.Errorf("create sampler: %w", err)
	}

	traceEndpoint, err := signalEndpoint(cfg.ExporterEndpoint, "/v1/traces")
	if err != nil {
		return nil, fmt.Errorf("create trace endpoint: %w", err)
	}

	metricEndpoint, err := signalEndpoint(cfg.ExporterEndpoint, "/v1/metrics")
	if err != nil {
		return nil, fmt.Errorf("create metric endpoint: %w", err)
	}

	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(traceEndpoint))
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res), sdktrace.WithSampler(sampler))

	metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(metricEndpoint))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create metric exporter: %w", err), tracerProvider.Shutdown(ctx))
	}

	metricReader := sdkmetric.NewPeriodicReader(metricExporter)

	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader), sdkmetric.WithResource(res))

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error("OpenTelemetry background error", slog.Any("error", err))
	}))

	return &Telemetry{
		Logger:         logger,
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	}, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	return errors.Join(t.MeterProvider.Shutdown(ctx), t.TracerProvider.Shutdown(ctx))
}

func newResource(cfg config.TelemetryConfig) (*resource.Resource, error) {
	attributes, err := parseResourceAttributes(cfg.ResourceAttributes)
	if err != nil {
		return nil, err
	}

	attributes = append(attributes, semconv.ServiceName(cfg.ServiceName))

	return resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, attributes...))
}

func parseResourceAttributes(raw string) ([]attribute.KeyValue, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	attributes := make([]attribute.KeyValue, 0, len(parts))

	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("invalid resource attribute %q", part)
		}

		attributes = append(
			attributes,
			attribute.String(key, value),
		)
	}

	return attributes, nil
}

func newSampler(name string) (sdktrace.Sampler, error) {
	switch name {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	default:
		return nil, fmt.Errorf("unsupported traces sampler %q", name)
	}
}

func signalEndpoint(baseEndpoint, signalPath string) (string, error) {
	parsed, err := url.Parse(baseEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}

	if parsed.Scheme != "http" &&
		parsed.Scheme != "https" {
		return "", fmt.Errorf("endpoint scheme must be http or https")
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("endpoint host is required")
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("endpoint must not contain query or fragment")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + signalPath

	return parsed.String(), nil
}
