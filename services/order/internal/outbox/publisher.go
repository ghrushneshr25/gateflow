package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/kafkax"
	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"github.com/ghrushneshrathod/gateflow/libs/go/testhooks"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// Metrics are process-local counters (low cardinality).
type Metrics struct {
	PublishOK   atomic.Int64
	PublishFail atomic.Int64
	Claimed     atomic.Int64
}

// Publisher claims unpublished outbox rows and writes to Kafka.
// Order confirmation never waits on this component.
type Publisher struct {
	Store    *store.Store
	Producer *kafkax.Producer
	WorkerID string
	Lease    time.Duration
	Batch    int
	Interval time.Duration
	Log      *slog.Logger
	Metrics  *Metrics
	Topic    string
}

func (p *Publisher) Run(ctx context.Context) {
	if p.Lease <= 0 {
		p.Lease = 30 * time.Second
	}
	if p.Batch <= 0 {
		p.Batch = 20
	}
	if p.Interval <= 0 {
		p.Interval = 500 * time.Millisecond
	}
	if p.Topic == "" {
		p.Topic = kafkax.TopicOrderConfirmed
	}
	if p.Metrics == nil {
		p.Metrics = &Metrics{}
	}
	if p.Log == nil {
		p.Log = slog.Default()
	}
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.PublishOnce(ctx); err != nil {
				p.Log.Debug("outbox publish cycle", "err", err)
			}
		}
	}
}

// PublishOnce claims and publishes one batch. DB txn is not held during Kafka I/O.
func (p *Publisher) PublishOnce(ctx context.Context) error {
	if p.Log == nil {
		p.Log = slog.Default()
	}
	if p.Metrics == nil {
		p.Metrics = &Metrics{}
	}
	if p.Topic == "" {
		p.Topic = kafkax.TopicOrderConfirmed
	}
	if p.Lease <= 0 {
		p.Lease = 30 * time.Second
	}
	if p.Batch <= 0 {
		p.Batch = 20
	}
	rows, err := p.Store.ClaimOutbox(ctx, p.WorkerID, p.Lease, p.Batch)
	if err != nil {
		return err
	}
	p.Metrics.Claimed.Add(int64(len(rows)))
	for _, row := range rows {
		if err := p.publishRow(ctx, row); err != nil {
			p.Log.Debug("publish row failed", "event_id", row.EventID, "err", err)
		}
	}
	return nil
}

func (p *Publisher) publishRow(ctx context.Context, row store.OutboxRow) error {
	if p.Log == nil {
		p.Log = slog.Default()
	}
	if p.Metrics == nil {
		p.Metrics = &Metrics{}
	}
	var payload any
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		_ = p.Store.FailOutboxClaim(ctx, row.ID, "invalid payload json: "+err.Error())
		p.Metrics.PublishFail.Add(1)
		metrics.OutboxPublish("fail")
		return err
	}
	env := map[string]any{
		"event_id":       row.EventID,
		"event_type":     row.EventType,
		"schema_version": row.SchemaVersion,
		"occurred_at":    row.CreatedAt.UTC().Format(time.RFC3339Nano),
		"producer":       "order",
		"correlation_id": row.CorrelationID,
		"payload":        payload,
	}
	body, err := json.Marshal(env)
	if err != nil {
		_ = p.Store.FailOutboxClaim(ctx, row.ID, err.Error())
		p.Metrics.PublishFail.Add(1)
		metrics.OutboxPublish("fail")
		return err
	}
	headers := map[string]string{
		"event_id":        row.EventID,
		"event_type":      row.EventType,
		"correlation_id":  row.CorrelationID,
	}
	carrier := telemetry.CarrierToMap(row.TraceCarrier)
	// Bind correlation for structured logs around publish.
	pubBase := ctx
	if row.CorrelationID != "" {
		pubBase = httpx.WithCorrelation(ctx, row.CorrelationID)
	}
	pubCtx, cancel := context.WithTimeout(pubBase, 5*time.Second)
	err = p.Producer.PublishWithTrace(pubCtx, p.Topic, row.AggregateID, headers, body, carrier)
	cancel()
	log := httpx.Logger(pubBase, p.Log)
	if err != nil {
		_ = p.Store.FailOutboxClaim(ctx, row.ID, err.Error())
		p.Metrics.PublishFail.Add(1)
		metrics.OutboxPublish("fail")
		log.Debug("outbox publish failed", "event_id", row.EventID, "err", err)
		return err
	}
	// P4.5: Kafka accepted write; PG published_at not yet set.
	testhooks.MaybeCrashOn("after_kafka_ack", row.EventID)
	if err := p.Store.MarkOutboxPublished(ctx, row.ID, time.Now().UTC()); err != nil {
		// Kafka already has the message; stable event_id + consumer dedupe handles republish.
		p.Metrics.PublishFail.Add(1)
		metrics.OutboxPublish("fail")
		log.Error("mark published failed after kafka ack", "event_id", row.EventID, "err", err)
		return err
	}
	p.Metrics.PublishOK.Add(1)
	metrics.OutboxPublish("ok")
	log.Info("outbox published", "event_id", row.EventID, "event_type", row.EventType)
	return nil
}

func (p *Publisher) BacklogAge(ctx context.Context) (time.Duration, int, error) {
	return p.Store.OutboxBacklog(ctx)
}
