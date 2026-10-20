package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/kafkax"
	"github.com/ghrushneshrathod/gateflow/libs/go/testhooks"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// Consumer reads order.confirmed events and applies a durable effect once per event_id.
// Kafka offset is committed only after the DB transaction succeeds (P43-FR-09).
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
		eventID := msg.Headers["event_id"]
		var env struct {
			EventID string          `json:"event_id"`
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.Unmarshal(msg.Value, &env)
		if eventID == "" {
			eventID = env.EventID
		}
		if eventID == "" {
			c.Errors.Add(1)
			c.Log.Warn("skip message without event_id")
			// poison: commit to avoid infinite loop; quarantine path is publisher-side
			_ = c.Reader.Commit(ctx, msg)
			continue
		}
		orderID := extractOrderID(msg.Value, env.Payload)
		testhooks.MaybeCrashOn("before_consumer_db", eventID)
		first, err := c.Store.ApplyOrderConfirmedEffect(ctx, eventID, orderID)
		if err != nil {
			c.Errors.Add(1)
			c.Log.Error("apply effect failed; not committing offset", "event_id", eventID, "err", err)
			// Do not commit — message may be redelivered
			time.Sleep(200 * time.Millisecond)
			continue
		}
		// P4.5: DB committed; Kafka offset not yet committed
		testhooks.MaybeCrashOn("after_consumer_db", eventID)
		if err := c.Reader.Commit(ctx, msg); err != nil {
			c.Errors.Add(1)
			c.Log.Error("commit offset failed after db success", "event_id", eventID, "err", err)
			// DB effect already applied; redelivery will dedupe
			continue
		}
		if !first {
			c.Dupes.Add(1)
			continue
		}
		c.Handled.Add(1)
		c.Log.Info("processed order confirmed event", "event_id", eventID, "order_id", orderID)
	}
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
