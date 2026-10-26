package kafkax

import (
	"context"

	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PublishWithTrace publishes a Kafka message, injecting W3C trace context into headers.
// carrier is optional durable context from outbox (checkout span); may be nil.
// Business headers (event_id, etc.) are preserved; trace headers are added/overwritten.
func (p *Producer) PublishWithTrace(ctx context.Context, topic, key string, headers map[string]string, value []byte, carrier map[string]string) error {
	if headers == nil {
		headers = map[string]string{}
	} else {
		// copy so callers' maps are not mutated unexpectedly beyond inject
		cp := make(map[string]string, len(headers)+4)
		for k, v := range headers {
			cp[k] = v
		}
		headers = cp
	}
	// Seed headers with durable carrier first (originating checkout), then start span.
	for k, v := range carrier {
		if _, exists := headers[k]; !exists {
			headers[k] = v
		}
	}
	ctx, span := telemetry.StartProducerSpan(ctx, carrier, "messaging.publish")
	defer span.End()
	span.SetAttributes(
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination", topic),
		attribute.String("messaging.operation", "publish"),
	)
	if et := headers["event_type"]; et != "" {
		span.SetAttributes(attribute.String("messaging.gateflow.event_type", et))
	}
	if eid := headers["event_id"]; eid != "" {
		span.SetAttributes(attribute.String("messaging.gateflow.event_id", eid))
	}
	// Inject active (producer) context so consumer can link/continue.
	telemetry.InjectMap(ctx, headers)
	err := p.Publish(ctx, topic, key, headers, value)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// ProcessContext builds a consumer processing context from Kafka headers (never fails).
func ProcessContext(parent context.Context, headers map[string]string) (context.Context, trace.Span) {
	return telemetry.StartConsumerSpan(parent, headers, "messaging.process")
}
