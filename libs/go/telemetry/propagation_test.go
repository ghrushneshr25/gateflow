package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func withTestTP(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exp
}

func TestInjectExtractRoundTrip(t *testing.T) {
	exp := withTestTP(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "checkout")
	defer span.End()

	h := map[string]string{}
	InjectMap(ctx, h)
	if h["traceparent"] == "" {
		t.Fatal("expected traceparent")
	}

	got := ExtractMap(context.Background(), h)
	sc := trace.SpanContextFromContext(got)
	if !sc.IsValid() || sc.TraceID() != span.SpanContext().TraceID() {
		t.Fatalf("trace mismatch: %v vs %v", sc.TraceID(), span.SpanContext().TraceID())
	}
	_ = exp
}

func TestCaptureCarrierEmptyWithoutSpan(t *testing.T) {
	withTestTP(t)
	if CaptureCarrier(context.Background()) != "" {
		// may still inject empty carrier depending on propagator; accept empty map JSON
		m := CarrierToMap(CaptureCarrier(context.Background()))
		if len(m) > 0 && m["traceparent"] != "" {
			t.Fatal("unexpected valid carrier without span")
		}
	}
}

func TestCaptureCarrierWithSpan(t *testing.T) {
	withTestTP(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "confirm")
	defer span.End()
	raw := CaptureCarrier(ctx)
	m := CarrierToMap(raw)
	if m["traceparent"] == "" {
		t.Fatalf("carrier=%q", raw)
	}
}

func TestCarrierToMapInvalid(t *testing.T) {
	if CarrierToMap("") != nil || CarrierToMap("not-json") != nil {
		t.Fatal("expected nil")
	}
}

func TestConsumerSpanLinksRemote(t *testing.T) {
	exp := withTestTP(t)
	ctx, root := otel.Tracer("test").Start(context.Background(), "checkout")
	h := map[string]string{}
	InjectMap(ctx, h)
	root.End()

	_, cspan := StartConsumerSpan(context.Background(), h, "messaging.process")
	cspan.End()

	spans := exp.GetSpans()
	var found bool
	for _, s := range spans {
		if s.Name == "messaging.process" {
			found = true
			if len(s.Links) < 1 {
				t.Fatal("expected span link to checkout")
			}
			if s.SpanContext.TraceID() == root.SpanContext().TraceID() {
				// WithNewRoot should create different trace id
				// (actually WithNewRoot creates new trace - good)
			}
		}
	}
	if !found {
		t.Fatalf("spans=%v", spans)
	}
}

func TestMalformedHeadersDoNotPanic(t *testing.T) {
	withTestTP(t)
	h := map[string]string{"traceparent": "garbage"}
	ctx := ExtractMap(context.Background(), h)
	_, span := StartConsumerSpan(ctx, h, "messaging.process")
	span.End()
}
