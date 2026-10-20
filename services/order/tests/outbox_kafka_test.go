//go:build integration && kafka

package tests

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/kafkax"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/outbox"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func kafkaBrokers() []string {
	return kafkax.ParseBrokers(os.Getenv("KAFKA_BROKERS"))
}

func orderStoreKafka(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("ORDER_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	if os.Getenv("KAFKA_BROKERS") == "" && os.Getenv("SKIP_KAFKA") == "1" {
		t.Skip("kafka skipped")
	}
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), url, "order")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	brokers := kafkaBrokers()
	if err := kafkax.EnsureTopic(brokers, kafkax.TopicOrderConfirmed, 1); err != nil {
		t.Skipf("kafka not available: %v", err)
	}
	return store.New(pool)
}

func TestP43_PublishAndDedupe(t *testing.T) {
	st := orderStoreKafka(t)
	ctx := context.Background()
	orderID := ids.New("ord")
	eventID := "evt_order_confirmed_" + orderID
	payload, _ := json.Marshal(map[string]any{
		"order_id": orderID, "buyer_id": "b1", "session_id": "s1", "quantity": 1,
	})
	if err := st.InsertOutboxForTest(ctx, eventID, "order.order.confirmed", orderID, "corr", payload, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	brokers := kafkaBrokers()
	prod := kafkax.NewProducer(brokers)
	t.Cleanup(func() { _ = prod.Close() })
	pub := &outbox.Publisher{
		Store: st, Producer: prod, WorkerID: "test-pub", Metrics: &outbox.Metrics{},
		Interval: time.Hour, // manual
	}
	if err := pub.PublishOnce(ctx); err != nil {
		t.Fatal(err)
	}
	published, err := st.IsOutboxPublished(ctx, eventID)
	if err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}

	// Consumer processes once
	reader := kafkax.NewConsumer(brokers, kafkax.TopicOrderConfirmed, "test-dedupe-"+ids.New("g"))
	t.Cleanup(func() { _ = reader.Close() })
	cons := &outbox.Consumer{Store: st, Reader: reader}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	go cons.Run(cctx)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if cons.Handled.Load() >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if cons.Handled.Load() < 1 {
		t.Fatalf("handled=%d dupes=%d err=%d", cons.Handled.Load(), cons.Dupes.Load(), cons.Errors.Load())
	}
	n, err := st.CountConfirmedEffects(ctx, orderID)
	if err != nil || n != 1 {
		t.Fatalf("effects=%d err=%v", n, err)
	}

	// Simulate republish / redelivery of same event_id via Apply again
	first, err := st.ApplyOrderConfirmedEffect(ctx, eventID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if first {
		t.Fatal("expected duplicate")
	}
	n2, _ := st.CountConfirmedEffects(ctx, orderID)
	if n2 != 1 {
		t.Fatalf("effects after dupe=%d", n2)
	}
}

func TestP43_ConfirmIndependentOfKafka(t *testing.T) {
	// Confirm path does not call Kafka — unit-level: Confirm with store only.
	st := orderStoreKafka(t)
	ctx := context.Background()
	now := time.Now().UTC()
	o := store.Order{
		ID: ids.New("ord"), BuyerID: ids.New("b"), SessionID: ids.New("s"),
		TicketTypeID: "tt", PoolID: "p", Quantity: 1, Status: "PAYMENT_SUCCEEDED",
		EntitlementID: "e", CheckoutOperationID: ids.New("c"), InventoryOperationID: ids.New("i"),
		PaymentOperationID: ids.New("p"), AmountCents: 1, Currency: "USD", MaxPerBuyer: 4,
		IdempotencyKey: ids.New("k"), RequestHash: "h",
	}
	o, err := st.CreateWithHold(ctx, o, ids.New("hold"), now)
	if err != nil {
		t.Fatal(err)
	}
	o.Status = "PAYMENT_SUCCEEDED"
	_ = st.Update(ctx, o)
	// No kafka required
	eventID := "evt_order_confirmed_" + o.ID
	got, err := st.Confirm(ctx, o.ID, eventID, "corr", map[string]any{
		"order_id": o.ID, "buyer_id": o.BuyerID, "session_id": o.SessionID, "quantity": 1,
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "CONFIRMED" {
		t.Fatal(got.Status)
	}
	pub, err := st.IsOutboxPublished(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if pub {
		t.Fatal("should be unpublished until publisher runs")
	}
}

func TestP43_ConcurrentClaimNoLoss(t *testing.T) {
	st := orderStoreKafka(t)
	ctx := context.Background()
	// seed several events
	for i := 0; i < 5; i++ {
		oid := ids.New("ord")
		eid := "evt_order_confirmed_" + oid
		payload, _ := json.Marshal(map[string]any{"order_id": oid, "buyer_id": "b", "session_id": "s", "quantity": 1})
		_ = st.InsertOutboxForTest(ctx, eid, "order.order.confirmed", oid, "c", payload, time.Now().UTC())
	}
	brokers := kafkaBrokers()
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			prod := kafkax.NewProducer(brokers)
			defer prod.Close()
			pub := &outbox.Publisher{Store: st, Producer: prod, WorkerID: ids.New("w"), Metrics: &outbox.Metrics{}}
			for i := 0; i < 5; i++ {
				_ = pub.PublishOnce(ctx)
				time.Sleep(50 * time.Millisecond)
			}
		}(w)
	}
	wg.Wait()
	// All should eventually be published or still claimed — wait one more drain
	prod := kafkax.NewProducer(brokers)
	defer prod.Close()
	pub := &outbox.Publisher{Store: st, Producer: prod, WorkerID: "final", Metrics: &outbox.Metrics{}}
	for i := 0; i < 10; i++ {
		_ = pub.PublishOnce(ctx)
		_, n, _ := st.OutboxBacklog(ctx)
		if n == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, n, err := st.OutboxBacklog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("backlog remaining=%d", n)
	}
}
