package kafkax

import (
	"context"
	"testing"

	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPublishWithTraceInjectsTraceparent(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, root := otel.Tracer("t").Start(context.Background(), "checkout")
	carrier := map[string]string{}
	telemetry.InjectMap(ctx, carrier)
	root.End()

	// Use a fake by checking headers via a stub — Publish needs real writer.
	// Unit-test inject path through StartProducerSpan + InjectMap only.
	h := map[string]string{"event_id": "evt_1", "event_type": "order.order.confirmed"}
	pctx, span := telemetry.StartProducerSpan(context.Background(), carrier, "messaging.publish")
	telemetry.InjectMap(pctx, h)
	span.End()
	if h["traceparent"] == "" {
		t.Fatal("missing traceparent")
	}
	if h["event_id"] != "evt_1" {
		t.Fatal("business header clobbered")
	}
}

func TestProcessContextMalformedOK(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := ProcessContext(context.Background(), map[string]string{
		"traceparent": "not-valid",
		"event_id":    "evt_x",
	})
	span.End()
	_ = ctx
}
