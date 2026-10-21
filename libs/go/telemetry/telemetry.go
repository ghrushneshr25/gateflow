// Package telemetry initializes OpenTelemetry tracing for Gateflow services.
//
// Enable with GATEFLOW_OTEL_ENABLED=1 (or true). Export via OTLP/HTTP to
// GATEFLOW_OTEL_ENDPOINT (default localhost:4318). Telemetry failure never
// blocks booking: exporters are async/batched and Init errors degrade to no-op.
package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Init configures global TracerProvider when enabled. Returns shutdown func.
func Init(log *slog.Logger, service string) func() {
	if log == nil {
		log = slog.Default()
	}
	if !enabled() {
		log.Info("telemetry disabled", "service", service, "hint", "set GATEFLOW_OTEL_ENABLED=1")
		return func() {}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	endpoint := strings.TrimSpace(os.Getenv("GATEFLOW_OTEL_ENDPOINT"))
	if endpoint == "" {
		endpoint = "localhost:4318"
	}
	// otlptracehttp wants host:port without scheme by default when WithEndpoint is used.
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "local"
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		log.Warn("telemetry exporter init failed; continuing without traces", "service", service, "err", err)
		return func() {}
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(service),
			semconv.DeploymentEnvironment(env),
		),
	)
	if err != nil {
		log.Warn("telemetry resource failed", "err", err)
		res = resource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithMaxExportBatchSize(128),
			sdktrace.WithBatchTimeout(2*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // local/dev; production may use ParentBased
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Info("telemetry initialized",
		"service", service,
		"endpoint", endpoint,
		"environment", env,
	)

	return func() {
		shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shCancel()
		if err := tp.Shutdown(shCtx); err != nil {
			log.Warn("telemetry shutdown", "err", err)
		}
	}
}

func enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GATEFLOW_OTEL_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// Enabled reports whether OTel export is configured for this process.
func Enabled() bool { return enabled() }
