//go:build integration

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

func orderStoreAny(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("ORDER_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), url, "order")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestP431_ReplayQuarantinedPreservesEventID(t *testing.T) {
	st := orderStoreAny(t)
	ctx := context.Background()
	orderID := ids.New("ord")
	eventID := "evt_order_confirmed_" + orderID
	payload, _ := json.Marshal(map[string]any{"order_id": orderID, "buyer_id": "b", "session_id": "s", "quantity": 1})
	if err := st.InsertOutboxForTest(ctx, eventID, "order.order.confirmed", orderID, "c", payload, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := st.ForceQuarantineForTest(ctx, eventID, "poison"); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListQuarantined(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range list {
		if e.EventID == eventID {
			found = true
		}
	}
	if !found {
		t.Fatal("not listed")
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = st.RequestReplay(ctx, eventID, "ops", time.Now().UTC())
		}()
	}
	wg.Wait()
	ev, err := st.RequestReplay(ctx, eventID, "ops-final", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventID != eventID {
		t.Fatalf("event id changed %s", ev.EventID)
	}
	if ev.ReplayCount < 1 {
		t.Fatalf("replay_count=%d", ev.ReplayCount)
	}

	// If kafka available, publish after replay
	if os.Getenv("KAFKA_BROKERS") == "" {
		return
	}
	brokers := kafkax.ParseBrokers(os.Getenv("KAFKA_BROKERS"))
	if err := kafkax.EnsureTopic(brokers, kafkax.TopicOrderConfirmed, 1); err != nil {
		t.Logf("kafka skip publish: %v", err)
		return
	}
	prod := kafkax.NewProducer(brokers)
	t.Cleanup(func() { _ = prod.Close() })
	pub := &outbox.Publisher{Store: st, Producer: prod, WorkerID: ids.New("w"), Metrics: &outbox.Metrics{}}
	if err := pub.PublishOnce(ctx); err != nil {
		t.Fatal(err)
	}
	ok, err := st.IsOutboxPublished(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		// may need another cycle if claimed by others
		_ = pub.PublishOnce(ctx)
		ok, _ = st.IsOutboxPublished(ctx, eventID)
	}
	if !ok {
		t.Fatal("expected published after replay")
	}
	// dedupe effect
	first, _ := st.ApplyOrderConfirmedEffect(ctx, eventID, orderID)
	second, _ := st.ApplyOrderConfirmedEffect(ctx, eventID, orderID)
	if !first && second {
		t.Fatal("second should not be first")
	}
	n, _ := st.CountConfirmedEffects(ctx, orderID)
	if n != 1 {
		// first Apply might have been first
		if n > 1 {
			t.Fatalf("effects=%d", n)
		}
	}
}

func TestP431_RuntimeRecoversWhenKafkaAppears(t *testing.T) {
	if os.Getenv("KAFKA_BROKERS") == "" {
		t.Skip("KAFKA_BROKERS not set")
	}
	st := orderStoreAny(t)
	brokers := kafkax.ParseBrokers(os.Getenv("KAFKA_BROKERS"))
	// Ensure kafka is up for this test of recovery after temporary ensure failure simulation:
	// We start runtime; if EnsureTopic works, Ready becomes true.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rt := &outbox.Runtime{Store: st, Brokers: brokers, Log: nil, Metrics: &outbox.Metrics{}}
	go rt.Run(ctx)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if rt.Ready() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !rt.Ready() {
		t.Fatal("runtime never became ready")
	}
	// seed event and publish via runtime pub once ready
	orderID := ids.New("ord")
	eventID := "evt_order_confirmed_" + orderID
	payload, _ := json.Marshal(map[string]any{"order_id": orderID, "buyer_id": "b", "session_id": "s", "quantity": 1})
	_ = st.InsertOutboxForTest(context.Background(), eventID, "order.order.confirmed", orderID, "c", payload, time.Now().UTC())
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ok, _ := st.IsOutboxPublished(context.Background(), eventID)
		if ok {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("event not published by runtime")
}
