package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/kafkax"
	"github.com/ghrushneshrathod/gateflow/libs/go/testhooks"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Consumer reads order.confirmed events and applies a durable effect once per event_id.
// Kafka offset is committed only after the DB transaction succeeds (P43-FR-09).
//
// Trace policy (P5.4.2): each message starts a NEW root consumer span linked to the
// producer/checkout context extracted from Kafka W3C headers. Missing/malformed
// headers never block processing.
type Consumer struct {
	Store   *store.Store
	Reader  *kafkax.Consumer
	Log     *slog.Logger
	Handled atomic.Int64
	Dupes   atomic.Int64
	Errors  atomic.Int64
}

func (c *Consumer) Run(ctx context.Context) {
	if c.Log == nil {
		c.Log = slog.Default()
	}
	for {
		if ctx.Err() != nil {
			return
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		msg, err := c.Reader.Fetch(fetchCtx)
		cancel()
		if err != nil {
			continue
		}
		c.processMessage(ctx, msg)
	}
}

func (c *Consumer) processMessage(parent context.Context, msg kafkax.Message) {
	pctx, span := kafkax.ProcessContext(parent, msg.Headers)
	defer span.End()

	eventID := msg.Headers["event_id"]
	var env struct {
		EventID       string          `json:"event_id"`
		CorrelationID string          `json:"correlation_id"`
		Payload       json.RawMessage `json:"payload"`
	}
	_ = json.Unmarshal(msg.Value, &env)
	if eventID == "" {
		eventID = env.EventID
	}
	corr := msg.Headers["correlation_id"]
	if corr == "" {
		corr = env.CorrelationID
	}
	if corr != "" && httpx.ValidCorrelationID(corr) {
		pctx = httpx.WithCorrelation(pctx, corr)
	}
	log := httpx.Logger(pctx, c.Log)
	span.SetAttributes(
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.operation", "process"),
	)
	if eventID != "" {
		span.SetAttributes(attribute.String("messaging.gateflow.event_id", eventID))
	}

	if eventID == "" {
		c.Errors.Add(1)
		log.Warn("skip message without event_id")
		// poison: commit to avoid infinite loop; quarantine path is publisher-side
		_ = c.Reader.Commit(parent, msg)
		span.SetStatus(codes.Error, "missing event_id")
		return
	}
	orderID := extractOrderID(msg.Value, env.Payload)
	testhooks.MaybeCrashOn("before_consumer_db", eventID)
	first, err := c.Store.ApplyOrderConfirmedEffect(pctx, eventID, orderID)
	if err != nil {
		c.Errors.Add(1)
		log.Error("apply effect failed; not committing offset", "event_id", eventID, "err", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// Do not commit — message may be redelivered
		time.Sleep(200 * time.Millisecond)
		return
	}
	// P4.5: DB committed; Kafka offset not yet committed
	testhooks.MaybeCrashOn("after_consumer_db", eventID)
	if err := c.Reader.Commit(parent, msg); err != nil {
		c.Errors.Add(1)
		log.Error("commit offset failed after db success", "event_id", eventID, "err", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "commit failed")
		// DB effect already applied; redelivery will dedupe
		return
	}
	if !first {
		c.Dupes.Add(1)
		span.SetAttributes(attribute.String("messaging.gateflow.outcome", "duplicate"))
		log.Info("duplicate order confirmed event", "event_id", eventID, "order_id", orderID)
		return
	}
	c.Handled.Add(1)
	span.SetAttributes(attribute.String("messaging.gateflow.outcome", "applied"))
	span.SetStatus(codes.Ok, "")
	log.Info("processed order confirmed event", "event_id", eventID, "order_id", orderID)
}

func extractOrderID(value, payloadJSON []byte) string {
	var payload struct {
		OrderID string `json:"order_id"`
	}
	if len(payloadJSON) > 0 && json.Unmarshal(payloadJSON, &payload) == nil && payload.OrderID != "" {
		return payload.OrderID
	}
	var bare map[string]any
	if json.Unmarshal(value, &bare) == nil {
		if p, ok := bare["payload"].(map[string]any); ok {
			if oid, ok := p["order_id"].(string); ok {
				return oid
			}
		}
	}
	return "unknown"
}
