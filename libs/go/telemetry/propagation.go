package telemetry

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// InjectMap writes W3C trace context from ctx into a string map (e.g. Kafka headers).
func InjectMap(ctx context.Context, headers map[string]string) {
	if headers == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
}

// ExtractMap returns a context with remote span context extracted from headers.
// Malformed/missing headers yield the parent ctx unchanged (never fails).
func ExtractMap(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(headers))
}

// CaptureCarrier serializes current W3C context for durable storage (outbox).
// Empty string when no valid span is active.
func CaptureCarrier(ctx context.Context) string {
	m := map[string]string{}
	InjectMap(ctx, m)
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// CarrierToMap parses a stored carrier JSON object. Invalid input → nil (safe).
func CarrierToMap(raw string) map[string]string {
	raw = trimSpace(raw)
	if raw == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// StartProducerSpan starts a producer span. If carrier has remote context, that
// becomes the parent (continues checkout trace through async publish).
func StartProducerSpan(ctx context.Context, carrier map[string]string, spanName string) (context.Context, trace.Span) {
	parent := ExtractMap(ctx, carrier)
	return otel.Tracer("gateflow/kafkax").Start(parent, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
	)
}

// StartConsumerSpan starts a NEW root consumer span linked to the extracted
// producer/checkout context. Async processing is not a false child of HTTP.
// Missing/malformed headers → plain consumer span with no link.
func StartConsumerSpan(ctx context.Context, headers map[string]string, spanName string) (context.Context, trace.Span) {
	remote := ExtractMap(context.Background(), headers)
	rsc := trace.SpanContextFromContext(remote)
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithNewRoot(),
	}
	if rsc.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{
			SpanContext: rsc,
			Attributes:  nil,
		}))
	}
	return otel.Tracer("gateflow/kafkax").Start(ctx, spanName, opts...)
}
