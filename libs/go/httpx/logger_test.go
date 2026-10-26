package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestLoggerIncludesCorrelationAndTrace(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := otel.Tracer("t").Start(context.Background(), "op")
	defer span.End()
	corr := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ctx = WithCorrelation(ctx, corr)

	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	Logger(ctx, base).Info("hello")
	out := buf.String()
	if !strings.Contains(out, corr) {
		t.Fatalf("missing correlation: %s", out)
	}
	if !strings.Contains(out, "trace_id") || !strings.Contains(out, "span_id") {
		t.Fatalf("missing trace fields: %s", out)
	}
	// redaction: never log auth-looking keys from helper itself
	if strings.Contains(strings.ToLower(out), "authorization") {
		t.Fatal("unexpected auth")
	}
}

func TestLoggerWithoutTraceStillHasCorrelation(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := WithCorrelation(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	Logger(ctx, base).Info("x")
	if !strings.Contains(buf.String(), "correlation_id") {
		t.Fatal(buf.String())
	}
	if strings.Contains(buf.String(), "trace_id") {
		t.Fatal("unexpected trace without span")
	}
}
